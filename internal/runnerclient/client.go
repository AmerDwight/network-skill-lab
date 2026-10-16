package runnerclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/provider/docker"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/runnerproto"
	"github.com/coder/websocket"
)

const (
	defaultPingInterval = 15 * time.Second
	defaultMinBackoff   = time.Second
	defaultMaxBackoff   = 30 * time.Second
	defaultRetention    = 2 * time.Minute
	dialTimeout         = 15 * time.Second
	welcomeTimeout      = 15 * time.Second
	cleanupTimeout      = 2 * time.Minute
)

var (
	ErrUnauthorized     = errors.New("the control plane rejected this runner")
	ErrProtocolMismatch = errors.New("the control plane speaks another protocol version")
)

type Options struct {
	URL          string
	Token        string
	Name         string
	Capabilities []runner.Env
	Capacity     int
	Version      string
	Provider     runner.Runner
	Logger       *slog.Logger
}

type client struct {
	opts         Options
	log          *slog.Logger
	pingInterval time.Duration
	minBackoff   time.Duration
	maxBackoff   time.Duration
	retention    time.Duration
	httpClient   *http.Client

	mu        sync.Mutex
	sandboxes map[runner.SandboxID]struct{}
	expiry    *time.Timer
}

// Run keeps the runner connected to the control plane until ctx is done. It returns
// nil after ctx is done and an error only when the control plane refuses the runner.
func Run(ctx context.Context, opts Options) error {
	return newClient(opts).run(ctx)
}

func newClient(opts Options) *client {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if len(opts.Capabilities) == 0 {
		opts.Capabilities = opts.Provider.Capabilities()
	}
	return &client{
		opts:         opts,
		log:          opts.Logger.With("runner", opts.Name),
		pingInterval: defaultPingInterval,
		minBackoff:   defaultMinBackoff,
		maxBackoff:   defaultMaxBackoff,
		retention:    defaultRetention,
		sandboxes:    map[runner.SandboxID]struct{}{},
	}
}

type leftoverCollector interface {
	Collect(ctx context.Context) (docker.Removed, error)
}

func (c *client) run(ctx context.Context) error {
	defer c.destroyAll("runner stopped")
	c.collectLeftovers(ctx)

	backoff := c.minBackoff
	for {
		welcomed, err := c.session(ctx)
		if ctx.Err() != nil {
			return nil
		}
		var closeErr websocket.CloseError
		if errors.As(err, &closeErr) {
			switch closeErr.Code {
			case runnerproto.CloseUnauthorized:
				return fmt.Errorf("%w: %s", ErrUnauthorized, closeErr.Reason)
			case runnerproto.CloseProtocolMismatch:
				return fmt.Errorf("%w: %s", ErrProtocolMismatch, closeErr.Reason)
			}
		}
		if welcomed {
			backoff = c.minBackoff
		}
		c.disconnected()
		c.log.Warn("not connected to the control plane", "error", err, "retry_in", backoff.String())

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, c.maxBackoff)
	}
}

// collectLeftovers removes what a previous process of this runner left on the provider.
// The process owns its instance exclusively and the control plane has already errored
// any attempt bound to the dead process, so nothing collected here is still in use.
func (c *client) collectLeftovers(ctx context.Context) {
	if collector, ok := c.opts.Provider.(leftoverCollector); ok {
		removed, err := collector.Collect(ctx)
		if err != nil {
			c.log.Warn("could not remove leftovers of a previous run", "error", err)
			return
		}
		c.log.Info(fmt.Sprintf("removed leftovers of a previous run: %d containers, %d networks", removed.Containers, removed.Networks),
			"containers", removed.Containers, "networks", removed.Networks)
		return
	}
	if err := c.opts.Provider.GC(ctx); err != nil {
		c.log.Warn("could not remove leftovers of a previous run", "error", err)
		return
	}
	c.log.Info("removed leftovers of a previous run")
}

func (c *client) session(ctx context.Context) (bool, error) {
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	ws, _, err := websocket.Dial(dialCtx, c.opts.URL, &websocket.DialOptions{
		HTTPClient: c.httpClient,
		HTTPHeader: http.Header{"Authorization": {"Bearer " + c.opts.Token}},
	})
	cancel()
	if err != nil {
		return false, fmt.Errorf("dial %s: %w", c.opts.URL, err)
	}
	defer func() { _ = ws.CloseNow() }()
	ws.SetReadLimit(readLimit)

	s := newSession(c, ws)
	hello := runnerproto.Hello{
		Proto:        runnerproto.Version,
		Name:         c.opts.Name,
		Version:      c.opts.Version,
		Capabilities: runnerproto.CapabilityNames(c.opts.Capabilities),
		Capacity:     c.opts.Capacity,
		Sandboxes:    c.sandboxList(),
	}
	if err := s.send(ctx, hello); err != nil {
		return false, err
	}
	welcome, err := readWelcome(ctx, ws)
	if err != nil {
		return false, err
	}
	c.connected()
	c.log.Info("connected to the control plane", "url", c.opts.URL, "runner_id", welcome.RunnerID, "reported_sandboxes", len(hello.Sandboxes))

	return true, s.serve(ctx)
}

func readWelcome(ctx context.Context, ws *websocket.Conn) (runnerproto.Welcome, error) {
	ctx, cancel := context.WithTimeout(ctx, welcomeTimeout)
	defer cancel()
	_, data, err := ws.Read(ctx)
	if err != nil {
		return runnerproto.Welcome{}, fmt.Errorf("read welcome: %w", err)
	}
	message, err := runnerproto.Decode(data)
	if err != nil {
		return runnerproto.Welcome{}, err
	}
	welcome, ok := message.(runnerproto.Welcome)
	if !ok {
		return runnerproto.Welcome{}, fmt.Errorf("expected welcome, got %T", message)
	}
	return welcome, nil
}

func (c *client) track(sb runner.SandboxID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sandboxes[sb] = struct{}{}
}

func (c *client) untrack(sb runner.SandboxID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.sandboxes, sb)
}

func (c *client) untrackAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.sandboxes)
}

func (c *client) active() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sandboxes)
}

func (c *client) sandboxList() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	list := make([]string, 0, len(c.sandboxes))
	for sb := range c.sandboxes {
		list = append(list, string(sb))
	}
	slices.Sort(list)
	return list
}

func (c *client) connected() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.expiry != nil {
		c.expiry.Stop()
		c.expiry = nil
	}
}

func (c *client) disconnected() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.expiry != nil || len(c.sandboxes) == 0 {
		return
	}
	c.log.Info("keeping sandboxes for a reconnect", "sandboxes", len(c.sandboxes), "for", c.retention)
	c.expiry = time.AfterFunc(c.retention, func() {
		c.destroyAll("no reconnect within the retention window")
	})
}

func (c *client) destroyAll(reason string) {
	c.mu.Lock()
	sandboxes := slices.Collect(maps.Keys(c.sandboxes))
	clear(c.sandboxes)
	if c.expiry != nil {
		c.expiry.Stop()
		c.expiry = nil
	}
	c.mu.Unlock()
	if len(sandboxes) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	for _, sb := range sandboxes {
		if err := c.opts.Provider.Destroy(ctx, sb); err != nil {
			c.log.Error("destroy sandbox", "sandbox", sb, "reason", reason, "error", err)
			continue
		}
		c.log.Info("destroyed sandbox", "sandbox", sb, "reason", reason)
	}
}
