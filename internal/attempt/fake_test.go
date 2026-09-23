package attempt

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"maps"
	"sync"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
)

const waitTimeout = 2 * time.Second

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type waiter struct {
	d  time.Duration
	ch chan time.Time
}

type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []waiter
}

func newClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.waiters = append(c.waiters, waiter{d: d, ch: ch})
	return ch
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (c *fakeClock) pending(d time.Duration) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, w := range c.waiters {
		if w.d == d {
			n++
		}
	}
	return n
}

func (c *fakeClock) fire(t *testing.T, d time.Duration) {
	t.Helper()
	waitUntil(t, "a timer armed for "+d.String(), func() bool { return c.pending(d) > 0 })

	c.mu.Lock()
	defer c.mu.Unlock()
	var kept, fired []waiter
	for _, w := range c.waiters {
		if w.d == d {
			fired = append(fired, w)
			continue
		}
		kept = append(kept, w)
	}
	c.waiters = kept
	for _, w := range fired {
		w.ch <- c.now
	}
}

type execCall struct {
	sandbox string
	node    string
	cmd     []string
	script  string
	env     map[string]string
}

type fakeRunner struct {
	mu         sync.Mutex
	steps      []string
	err        error
	block      chan struct{}
	provisions int
	destroyed  []string
	gcs        int
	execs      []execCall
	exec       func(call execCall, nth int) (runner.ExecResult, error)
}

var _ runner.Runner = (*fakeRunner)(nil)

func (f *fakeRunner) Capabilities() []runner.Env {
	return []runner.Env{runner.EnvContainer}
}

func (f *fakeRunner) Provision(ctx context.Context, spec runner.SandboxSpec) (runner.SandboxID, error) {
	f.mu.Lock()
	f.provisions++
	steps, err, block := f.steps, f.err, f.block
	f.mu.Unlock()

	for _, step := range steps {
		if spec.Progress != nil {
			spec.Progress(step)
		}
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if err != nil {
		return "", err
	}
	return runner.SandboxID(spec.AttemptID), nil
}

func (f *fakeRunner) Destroy(_ context.Context, sb runner.SandboxID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.destroyed = append(f.destroyed, string(sb))
	return nil
}

func (f *fakeRunner) GC(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gcs++
	return nil
}

func (f *fakeRunner) OpenTerminal(_ context.Context, _ runner.SandboxID, _ string) (runner.PTY, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeRunner) Exec(_ context.Context, sb runner.SandboxID, node string, cmd []string, opts runner.ExecOptions) (runner.ExecResult, error) {
	var script []byte
	if opts.Stdin != nil {
		read, err := io.ReadAll(opts.Stdin)
		if err != nil {
			return runner.ExecResult{}, err
		}
		script = read
	}
	call := execCall{sandbox: string(sb), node: node, cmd: cmd, script: string(script), env: maps.Clone(opts.Env)}

	f.mu.Lock()
	f.execs = append(f.execs, call)
	nth := len(f.execs)
	exec := f.exec
	f.mu.Unlock()

	if exec == nil {
		return runner.ExecResult{}, errors.New("not implemented")
	}
	return exec(call, nth)
}

func (f *fakeRunner) execCalls() []execCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]execCall(nil), f.execs...)
}

func (f *fakeRunner) Health(_ context.Context) runner.Health {
	return runner.Health{OK: true}
}

func (f *fakeRunner) provisionCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.provisions
}

func (f *fakeRunner) destroys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.destroyed...)
}

func (f *fakeRunner) gcCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gcs
}
