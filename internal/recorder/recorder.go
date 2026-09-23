package recorder

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/attempt"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

const (
	defaultInterval = 5 * time.Second
	pullTimeout     = 5 * time.Second
	storeTimeout    = 10 * time.Second
	commandLogPath  = "/var/log/nsl/commands.jsonl"
)

type Deps struct {
	Store    *store.Store
	Runner   runner.Runner
	Attempts *attempt.Service
	Interval time.Duration
	DataDir  string
	Logger   *slog.Logger
}

type Recorder struct {
	store    *store.Store
	runner   runner.Runner
	attempts *attempt.Service
	interval time.Duration
	dataDir  string
	log      *slog.Logger
	ticker   func(time.Duration) (<-chan time.Time, func())
	pulled   func()

	mu     sync.Mutex
	cancel context.CancelFunc
	loops  map[string]*loop

	wg        sync.WaitGroup
	closeOnce sync.Once
}

type loop struct {
	cancel context.CancelFunc
	stop   chan struct{}
	once   sync.Once
}

func New(deps Deps) *Recorder {
	if deps.Interval <= 0 {
		deps.Interval = defaultInterval
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &Recorder{
		store:    deps.Store,
		runner:   deps.Runner,
		attempts: deps.Attempts,
		interval: deps.Interval,
		dataDir:  deps.DataDir,
		log:      deps.Logger,
		ticker:   newTicker,
		loops:    map[string]*loop{},
	}
}

func newTicker(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTicker(d)
	return t.C, t.Stop
}

func (r *Recorder) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	events, unsubscribe := r.attempts.SubscribeAll()

	r.mu.Lock()
	r.cancel = cancel
	r.mu.Unlock()

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer unsubscribe()
		r.dispatch(ctx, events)
	}()
	r.log.Info("recorder started", "interval", r.interval, "data_dir", r.dataDir)
}

func (r *Recorder) Close() {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		if r.cancel != nil {
			r.cancel()
		}
		for id, l := range r.loops {
			delete(r.loops, id)
			l.cancel()
		}
		r.mu.Unlock()
		r.wg.Wait()
	})
}

func (r *Recorder) dispatch(ctx context.Context, events <-chan attempt.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if ev.Type != attempt.EventStatus {
				continue
			}
			switch ev.Status {
			case store.StatusRunning:
				r.startLoop(ctx, ev.AttemptID)
			case store.StatusProvisioning:
			default:
				r.stopLoop(ev.AttemptID)
			}
		}
	}
}

func (r *Recorder) startLoop(ctx context.Context, id string) {
	r.mu.Lock()
	if _, ok := r.loops[id]; ok {
		r.mu.Unlock()
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	l := &loop{cancel: cancel, stop: make(chan struct{})}
	r.loops[id] = l
	r.mu.Unlock()

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer r.forget(id)
		r.run(loopCtx, id, l.stop)
	}()
}

func (r *Recorder) stopLoop(id string) {
	r.mu.Lock()
	l, ok := r.loops[id]
	r.mu.Unlock()
	if ok {
		l.once.Do(func() { close(l.stop) })
	}
}

func (r *Recorder) forget(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if l, ok := r.loops[id]; ok {
		delete(r.loops, id)
		l.cancel()
	}
}

func (r *Recorder) run(ctx context.Context, id string, stop <-chan struct{}) {
	view, err := r.attempts.Get(ctx, id)
	if err != nil {
		r.log.Error("read attempt to record", "attempt", id, "error", err)
		return
	}

	ticks, stopTicker := r.ticker(r.interval)
	defer stopTicker()

	offsets := make(map[string]int64, len(view.Nodes))
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			r.pullAll(ctx, view, offsets)
			return
		case <-ticks:
			r.pullAll(ctx, view, offsets)
		}
	}
}

func (r *Recorder) pullAll(ctx context.Context, view attempt.View, offsets map[string]int64) {
	for _, node := range view.Nodes {
		r.pull(ctx, view, node.Name, offsets)
	}
	if r.pulled != nil {
		r.pulled()
	}
}

func (r *Recorder) pull(ctx context.Context, view attempt.View, node string, offsets map[string]int64) {
	offset := offsets[node]
	cmd := []string{"tail", "-c", "+" + strconv.FormatInt(offset+1, 10), commandLogPath}
	result, err := r.runner.Exec(ctx, runner.SandboxID(view.SandboxID), node, cmd, runner.ExecOptions{Timeout: pullTimeout})
	if err != nil {
		if ctx.Err() == nil {
			r.log.Warn("read command log", "attempt", view.Id, "node", node, "error", err)
		}
		return
	}
	if result.ExitCode != 0 {
		r.log.Debug("command log unavailable", "attempt", view.Id, "node", node, "exit_code", result.ExitCode)
		return
	}

	end := bytes.LastIndexByte(result.Stdout, '\n')
	if end < 0 {
		return
	}
	complete := result.Stdout[:end+1]

	entries, skipped := parse(view.Id, node, complete)
	if skipped > 0 {
		r.log.Warn("skipped unparseable command log lines", "attempt", view.Id, "node", node, "count", skipped)
	}
	if err := r.append(ctx, entries); err != nil {
		r.log.Error("append command log", "attempt", view.Id, "node", node, "error", err)
		return
	}
	offsets[node] = offset + int64(len(complete))
}

func (r *Recorder) append(ctx context.Context, entries []store.CommandEntry) error {
	if len(entries) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), storeTimeout)
	defer cancel()
	return r.store.CommandLog.AppendBatch(ctx, entries)
}

type logLine struct {
	TS   time.Time `json:"ts"`
	User string    `json:"user"`
	Cwd  string    `json:"cwd"`
	Cmd  string    `json:"cmd"`
	Exit int       `json:"exit"`
}

func parse(attemptID, node string, data []byte) ([]store.CommandEntry, int) {
	var (
		entries []store.CommandEntry
		skipped int
	)
	for _, raw := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var line logLine
		if err := json.Unmarshal(raw, &line); err != nil || line.TS.IsZero() {
			skipped++
			continue
		}
		entries = append(entries, store.CommandEntry{
			AttemptID: attemptID,
			Node:      node,
			TS:        line.TS,
			User:      line.User,
			CWD:       line.Cwd,
			Command:   line.Cmd,
			ExitCode:  line.Exit,
		})
	}
	return entries, skipped
}
