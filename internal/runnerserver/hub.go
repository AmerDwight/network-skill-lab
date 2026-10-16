package runnerserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/auth"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/runnerproto"
	"github.com/AmerDwight/network-skill-lab/internal/store"
	"github.com/coder/websocket"
)

const (
	defaultPingTimeout = 30 * time.Second
	touchInterval      = time.Minute
	helloTimeout       = 10 * time.Second
	replaceTimeout     = 10 * time.Second
	storeTimeout       = 5 * time.Second
	readLimit          = 16 << 20
)

var (
	ErrDisconnected = errors.New("runner disconnected")

	errUnauthorized = errors.New("invalid runner token")
)

type RunnerInfo struct {
	Name         string
	Capabilities []runner.Env
	Capacity     int
	Active       int
	Connected    bool
	LastSeen     time.Time
	Version      string
}

type entry struct {
	info RunnerInfo
	conn *conn
}

type Hub struct {
	runners     *store.Runners
	log         *slog.Logger
	pingTimeout time.Duration
	maxOutbox   int

	mu           sync.Mutex
	entries      map[string]*entry
	onDisconnect []func(name string)
	onReconnect  []func(name string, sandboxes []string)
}

func NewHub(runners *store.Runners, log *slog.Logger) *Hub {
	if log == nil {
		log = slog.Default()
	}
	return &Hub{
		runners:     runners,
		log:         log,
		pingTimeout: defaultPingTimeout,
		maxOutbox:   maxOutboxBytes,
		entries:     map[string]*entry{},
	}
}

func (h *Hub) OnDisconnect(fn func(name string)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onDisconnect = append(h.onDisconnect, fn)
}

func (h *Hub) OnReconnect(fn func(name string, sandboxes []string)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onReconnect = append(h.onReconnect, fn)
}

func (h *Hub) Runners() []RunnerInfo {
	h.mu.Lock()
	defer h.mu.Unlock()
	infos := make([]RunnerInfo, 0, len(h.entries))
	for _, e := range h.entries {
		info := e.info
		info.Capabilities = slices.Clone(info.Capabilities)
		infos = append(infos, info)
	}
	slices.SortFunc(infos, func(a, b RunnerInfo) int { return strings.Compare(a.Name, b.Name) })
	return infos
}

func (h *Hub) Get(name string) (runner.Runner, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.entries[name]
	if !ok || e.conn == nil {
		return nil, false
	}
	return e.conn.remote, true
}

// Disconnect closes the runner's connection with the unauthorized close code, so a
// runner whose token was just revoked stops instead of reconnecting.
func (h *Hub) Disconnect(name string) {
	h.mu.Lock()
	e, ok := h.entries[name]
	var c *conn
	if ok {
		c = e.conn
	}
	h.mu.Unlock()
	if c != nil {
		c.shutdown(runnerproto.CloseUnauthorized, "runner token revoked")
	}
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token, hasToken := bearerToken(r.Header.Get("Authorization"))
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		h.log.Warn("accept runner socket", "remote", r.RemoteAddr, "error", err)
		return
	}
	ws.SetReadLimit(readLimit)
	ctx := r.Context()

	hello, err := readHello(ctx, ws)
	if err != nil {
		h.log.Warn("runner handshake failed", "remote", r.RemoteAddr, "error", err)
		_ = ws.Close(runnerproto.CloseProtocolMismatch, "expected a hello message")
		return
	}
	log := h.log.With("runner", hello.Name, "remote", r.RemoteAddr)
	if hello.Proto != runnerproto.Version {
		log.Warn("runner speaks another protocol version", "runner_proto", hello.Proto, "proto", runnerproto.Version, "runner_version", hello.Version)
		reason := fmt.Sprintf("protocol version %d is not supported, this control plane speaks %d; install the matching nsl binary", hello.Proto, runnerproto.Version)
		_ = ws.Close(runnerproto.CloseProtocolMismatch, reason)
		return
	}
	record, err := h.authenticate(ctx, hello.Name, token, hasToken)
	if err != nil {
		log.Warn("runner rejected", "error", err)
		_ = ws.Close(runnerproto.CloseUnauthorized, errUnauthorized.Error())
		return
	}

	c := newConn(h, ws, record.ID, hello, log)
	if err := c.send(runnerproto.Welcome{RunnerID: record.ID}); err != nil {
		log.Warn("send welcome", "error", err)
	}
	h.register(c, hello)
	h.touch(c, hello)
	log.Info("runner connected", "version", hello.Version, "capacity", hello.Capacity, "capabilities", hello.Capabilities, "sandboxes", len(hello.Sandboxes))
	if len(hello.Sandboxes) > 0 {
		go h.reconnected(hello.Name, slices.Clone(hello.Sandboxes))
	}

	c.serve(ctx)
	h.unregister(c)
	log.Info("runner disconnected")
}

func bearerToken(header string) (string, bool) {
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", false
	}
	return token, true
}

func readHello(ctx context.Context, ws *websocket.Conn) (runnerproto.Hello, error) {
	ctx, cancel := context.WithTimeout(ctx, helloTimeout)
	defer cancel()
	kind, data, err := ws.Read(ctx)
	if err != nil {
		return runnerproto.Hello{}, fmt.Errorf("read hello: %w", err)
	}
	if kind != websocket.MessageText {
		return runnerproto.Hello{}, errors.New("hello is not a text message")
	}
	message, err := runnerproto.Decode(data)
	if err != nil {
		return runnerproto.Hello{}, err
	}
	hello, ok := message.(runnerproto.Hello)
	if !ok {
		return runnerproto.Hello{}, fmt.Errorf("first message is %T, not hello", message)
	}
	return hello, nil
}

func (h *Hub) authenticate(ctx context.Context, name, token string, hasToken bool) (store.Runner, error) {
	if !hasToken {
		return store.Runner{}, fmt.Errorf("%w: no bearer token", errUnauthorized)
	}
	record, err := h.runners.ByName(ctx, name)
	if err != nil {
		return store.Runner{}, fmt.Errorf("%w: %w", errUnauthorized, err)
	}
	if record.Revoked() {
		return store.Runner{}, fmt.Errorf("%w: runner %s is revoked", errUnauthorized, name)
	}
	ok, err := auth.VerifyPassword(record.TokenHash, token)
	if err != nil {
		return store.Runner{}, fmt.Errorf("%w: %w", errUnauthorized, err)
	}
	if !ok {
		return store.Runner{}, fmt.Errorf("%w: token mismatch for runner %s", errUnauthorized, name)
	}
	return record, nil
}

func (h *Hub) register(c *conn, hello runnerproto.Hello) {
	h.mu.Lock()
	var previous *conn
	if e, ok := h.entries[hello.Name]; ok {
		previous = e.conn
	}
	h.mu.Unlock()

	if previous != nil {
		c.log.Info("a new connection replaces the previous one")
		previous.shutdown(websocket.StatusGoingAway, "replaced by a new connection")
		select {
		case <-previous.finished:
		case <-time.After(replaceTimeout):
			c.log.Warn("the previous connection did not finish in time")
		}
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries[hello.Name] = &entry{
		conn: c,
		info: RunnerInfo{
			Name:         hello.Name,
			Capabilities: runnerproto.Capabilities(hello.Capabilities),
			Capacity:     hello.Capacity,
			Active:       len(hello.Sandboxes),
			Connected:    true,
			LastSeen:     time.Now(),
			Version:      hello.Version,
		},
	}
}

func (h *Hub) unregister(c *conn) {
	h.mu.Lock()
	e, ok := h.entries[c.name]
	current := ok && e.conn == c
	if current {
		e.conn = nil
		e.info.Connected = false
	}
	callbacks := slices.Clone(h.onDisconnect)
	h.mu.Unlock()

	if !current {
		return
	}
	for _, fn := range callbacks {
		fn(c.name)
	}
}

func (h *Hub) reconnected(name string, sandboxes []string) {
	h.mu.Lock()
	callbacks := slices.Clone(h.onReconnect)
	h.mu.Unlock()
	for _, fn := range callbacks {
		fn(name, sandboxes)
	}
}

func (h *Hub) pinged(c *conn, active int) {
	now := time.Now()
	h.mu.Lock()
	if e, ok := h.entries[c.name]; ok && e.conn == c {
		e.info.Active = active
		e.info.LastSeen = now
	}
	h.mu.Unlock()

	if now.Sub(c.lastTouch) >= touchInterval {
		h.touch(c, c.hello)
	}
}

func (h *Hub) touch(c *conn, hello runnerproto.Hello) {
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	c.lastTouch = time.Now()
	if err := h.runners.Touch(ctx, c.id, c.lastTouch, hello.Version, hello.Capacity, hello.Capabilities); err != nil {
		c.log.Warn("record runner heartbeat", "error", err)
	}
}
