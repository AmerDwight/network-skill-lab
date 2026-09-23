package checker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
)

const waitTimeout = 5 * time.Second

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type outcome struct {
	exitCode int
	timedOut bool
	err      error
}

func pass() outcome  { return outcome{} }
func fail() outcome  { return outcome{exitCode: 1} }
func fault() outcome { return outcome{err: errors.New("exec failed")} }
func hang() outcome  { return outcome{exitCode: -1, timedOut: true} }

type fakeRunner struct {
	mu        sync.Mutex
	outcomes  map[string][]outcome
	calls     map[string]int
	total     int
	block     map[string]chan struct{}
	blocked   map[string]int
	destroyed []string
}

var _ runner.Runner = (*fakeRunner)(nil)

func newFakeRunner() *fakeRunner {
	return &fakeRunner{
		outcomes: map[string][]outcome{},
		calls:    map[string]int{},
		block:    map[string]chan struct{}{},
		blocked:  map[string]int{},
	}
}

func execKey(node string, script []byte) string {
	return node + "\x00" + string(script)
}

func (f *fakeRunner) program(key string, outcomes ...outcome) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.outcomes[key] = outcomes
}

func (f *fakeRunner) blockOn(key string) chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	release := make(chan struct{})
	f.block[key] = release
	return release
}

func (f *fakeRunner) execs() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.total
}

func (f *fakeRunner) waitingOn(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.blocked[key]
}

func (f *fakeRunner) Exec(ctx context.Context, _ runner.SandboxID, node string, _ []string, opts runner.ExecOptions) (runner.ExecResult, error) {
	script, err := io.ReadAll(opts.Stdin)
	if err != nil {
		return runner.ExecResult{}, err
	}
	key := execKey(node, script)

	f.mu.Lock()
	f.total++
	n := f.calls[key]
	f.calls[key] = n + 1
	programmed := f.outcomes[key]
	release := f.block[key]
	if release != nil {
		f.blocked[key]++
	}
	f.mu.Unlock()

	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return runner.ExecResult{}, ctx.Err()
		}
		f.mu.Lock()
		f.blocked[key]--
		f.mu.Unlock()
	}

	if len(programmed) == 0 {
		return runner.ExecResult{ExitCode: 1}, nil
	}
	if n >= len(programmed) {
		n = len(programmed) - 1
	}
	out := programmed[n]
	if out.err != nil {
		return runner.ExecResult{}, out.err
	}
	return runner.ExecResult{ExitCode: out.exitCode, TimedOut: out.timedOut}, nil
}

func (f *fakeRunner) Capabilities() []runner.Env {
	return []runner.Env{runner.EnvContainer}
}

func (f *fakeRunner) Provision(_ context.Context, spec runner.SandboxSpec) (runner.SandboxID, error) {
	return runner.SandboxID(spec.AttemptID), nil
}

func (f *fakeRunner) Destroy(_ context.Context, sb runner.SandboxID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.destroyed = append(f.destroyed, string(sb))
	return nil
}

func (f *fakeRunner) GC(_ context.Context) error { return nil }

func (f *fakeRunner) OpenTerminal(_ context.Context, _ runner.SandboxID, _ string) (runner.PTY, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeRunner) Health(_ context.Context) runner.Health {
	return runner.Health{OK: true}
}
