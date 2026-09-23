package checker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/attempt"
	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

const (
	defaultInterval = 5 * time.Second
	defaultTimeout  = 10 * time.Second
	storeTimeout    = 10 * time.Second
)

type Deps struct {
	Store    *store.Store
	Runner   runner.Runner
	Attempts *attempt.Service
	Interval time.Duration
	Timeout  time.Duration
	Logger   *slog.Logger
}

type Checker struct {
	store    *store.Store
	runner   runner.Runner
	attempts *attempt.Service
	interval time.Duration
	timeout  time.Duration
	log      *slog.Logger
	ticker   func(time.Duration) (<-chan time.Time, func())
	swept    func()

	mu     sync.Mutex
	cancel context.CancelFunc
	loops  map[string]context.CancelFunc

	wg        sync.WaitGroup
	closeOnce sync.Once
}

func New(deps Deps) *Checker {
	if deps.Interval <= 0 {
		deps.Interval = defaultInterval
	}
	if deps.Timeout <= 0 {
		deps.Timeout = defaultTimeout
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &Checker{
		store:    deps.Store,
		runner:   deps.Runner,
		attempts: deps.Attempts,
		interval: deps.Interval,
		timeout:  deps.Timeout,
		log:      deps.Logger,
		ticker:   newTicker,
		loops:    map[string]context.CancelFunc{},
	}
}

func newTicker(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTicker(d)
	return t.C, t.Stop
}

func (c *Checker) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	events, unsubscribe := c.attempts.SubscribeAll()

	c.mu.Lock()
	c.cancel = cancel
	c.mu.Unlock()

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer unsubscribe()
		c.dispatch(ctx, events)
	}()
	c.log.Info("checker started", "interval", c.interval, "timeout", c.timeout)
}

func (c *Checker) Close() {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		if c.cancel != nil {
			c.cancel()
		}
		for id, cancel := range c.loops {
			delete(c.loops, id)
			cancel()
		}
		c.mu.Unlock()
		c.wg.Wait()
	})
}

func (c *Checker) dispatch(ctx context.Context, events <-chan attempt.Event) {
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
				c.startLoop(ctx, ev.AttemptID)
			case store.StatusProvisioning:
			default:
				c.stopLoop(ev.AttemptID)
			}
		}
	}
}

func (c *Checker) startLoop(ctx context.Context, id string) {
	c.mu.Lock()
	if _, ok := c.loops[id]; ok {
		c.mu.Unlock()
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	c.loops[id] = cancel
	c.mu.Unlock()

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer c.stopLoop(id)
		c.run(loopCtx, id)
	}()
}

func (c *Checker) stopLoop(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cancel, ok := c.loops[id]; ok {
		delete(c.loops, id)
		cancel()
	}
}

type checkpoint struct {
	id     string
	node   string
	script []byte
	env    map[string]string
}

type sweepState struct {
	last        map[string]string
	firstPassed map[string]time.Time
}

func (c *Checker) run(ctx context.Context, id string) {
	view, err := c.attempts.Get(ctx, id)
	if err != nil {
		c.log.Error("read attempt to check", "attempt", id, "error", err)
		return
	}
	checkpoints, err := loadCheckpoints(view)
	if err != nil {
		c.log.Error("load checkpoint scripts", "attempt", id, "error", err)
		return
	}
	if len(checkpoints) == 0 {
		return
	}

	ticks, stop := c.ticker(c.interval)
	defer stop()

	state := &sweepState{last: map[string]string{}, firstPassed: map[string]time.Time{}}
	done := make(chan bool, 1)
	sweeping := false

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			if sweeping {
				continue
			}
			sweeping = true
			c.wg.Add(1)
			go func() {
				defer c.wg.Done()
				done <- c.sweep(ctx, view, checkpoints, state)
			}()
		case passed := <-done:
			sweeping = false
			if c.swept != nil {
				c.swept()
			}
			if passed {
				return
			}
		}
	}
}

func (c *Checker) sweep(ctx context.Context, view attempt.View, checkpoints []checkpoint, state *sweepState) bool {
	statuses := make([]string, len(checkpoints))
	var wg sync.WaitGroup
	for i, cp := range checkpoints {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses[i] = c.check(ctx, view.Id, view.SandboxID, cp)
		}()
	}
	wg.Wait()

	if ctx.Err() != nil {
		return true
	}

	now := time.Now().UTC()
	passed := true
	for i, cp := range checkpoints {
		status := statuses[i]
		if status != store.CheckpointPass {
			passed = false
		} else if _, ok := state.firstPassed[cp.id]; !ok {
			state.firstPassed[cp.id] = now
		}

		run := store.CheckpointRun{
			AttemptID:    view.Id,
			CheckpointID: cp.id,
			LastStatus:   status,
			LastRunAt:    &now,
		}
		if first, ok := state.firstPassed[cp.id]; ok {
			run.FirstPassedAt = &first
		}
		if err := c.record(ctx, run); err != nil {
			c.log.Error("record checkpoint run", "attempt", view.Id, "checkpoint", cp.id, "error", err)
		}
		if previous, seen := state.last[cp.id]; !seen || previous != status {
			state.last[cp.id] = status
			c.attempts.PublishCheckpoint(view.Id, attempt.CheckpointEvent{
				Id:            cp.id,
				Status:        status,
				FirstPassedAt: run.FirstPassedAt,
			})
		}
	}

	if !passed {
		return false
	}
	c.finish(ctx, view.Id)
	return true
}

func (c *Checker) check(ctx context.Context, id, sandbox string, cp checkpoint) string {
	result, err := c.runner.Exec(ctx, runner.SandboxID(sandbox), cp.node, []string{"bash", "-s"}, runner.ExecOptions{
		Stdin:   bytes.NewReader(cp.script),
		Env:     cp.env,
		Timeout: c.timeout,
	})
	switch {
	case err != nil:
		if ctx.Err() == nil {
			c.log.Error("run checkpoint script", "attempt", id, "checkpoint", cp.id, "node", cp.node, "error", err)
		}
		return store.CheckpointError
	case result.TimedOut:
		c.log.Warn("checkpoint script timed out", "attempt", id, "checkpoint", cp.id, "node", cp.node, "timeout", c.timeout)
		return store.CheckpointError
	case result.ExitCode == 0:
		return store.CheckpointPass
	default:
		return store.CheckpointFail
	}
}

func (c *Checker) record(ctx context.Context, run store.CheckpointRun) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), storeTimeout)
	defer cancel()
	return c.store.CheckpointRuns.Upsert(ctx, run)
}

func (c *Checker) finish(ctx context.Context, id string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), storeTimeout)
	defer cancel()
	if err := c.attempts.Finish(ctx, id, store.StatusPassed); err != nil && !errors.Is(err, attempt.ErrTerminal) {
		c.log.Error("finish passed attempt", "attempt", id, "error", err)
	}
}

func loadCheckpoints(view attempt.View) ([]checkpoint, error) {
	env := content.ParamsEnv(view.Params)
	checkpoints := make([]checkpoint, 0, len(view.Lab.Checkpoints))
	for _, cp := range view.Lab.Checkpoints {
		script, err := os.ReadFile(filepath.Join(view.Lab.Dir, cp.Script))
		if err != nil {
			return nil, fmt.Errorf("checkpoint %s: %w", cp.Id, err)
		}
		nodeEnv := maps.Clone(env)
		nodeEnv["NSL_NODE"] = cp.Node
		checkpoints = append(checkpoints, checkpoint{id: cp.Id, node: cp.Node, script: script, env: nodeEnv})
	}
	return checkpoints, nil
}
