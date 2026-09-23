package recorder

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
)

const waitTimeout = 5 * time.Second

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeRunner struct {
	mu        sync.Mutex
	files     map[string]string
	tails     map[string][]int64
	destroyed int
}

var _ runner.Runner = (*fakeRunner)(nil)

func newFakeRunner() *fakeRunner {
	return &fakeRunner{files: map[string]string{}, tails: map[string][]int64{}}
}

func (f *fakeRunner) appendLines(node string, lines ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[node] += strings.Join(lines, "")
}

func (f *fakeRunner) offsets(node string) []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int64(nil), f.tails[node]...)
}

func (f *fakeRunner) destroys() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.destroyed
}

func (f *fakeRunner) Exec(_ context.Context, _ runner.SandboxID, node string, cmd []string, _ runner.ExecOptions) (runner.ExecResult, error) {
	if len(cmd) != 4 || cmd[0] != "tail" || cmd[1] != "-c" || cmd[3] != commandLogPath {
		return runner.ExecResult{}, errors.New("unexpected command " + strings.Join(cmd, " "))
	}
	start, err := strconv.ParseInt(strings.TrimPrefix(cmd[2], "+"), 10, 64)
	if err != nil {
		return runner.ExecResult{}, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.destroyed > 0 {
		return runner.ExecResult{}, errors.New("sandbox is gone")
	}
	f.tails[node] = append(f.tails[node], start)
	content, ok := f.files[node]
	if !ok {
		return runner.ExecResult{ExitCode: 1, Stderr: []byte("no such file")}, nil
	}
	if start-1 > int64(len(content)) {
		return runner.ExecResult{}, nil
	}
	return runner.ExecResult{Stdout: []byte(content[start-1:])}, nil
}

func (f *fakeRunner) Capabilities() []runner.Env {
	return []runner.Env{runner.EnvContainer}
}

func (f *fakeRunner) Provision(_ context.Context, spec runner.SandboxSpec) (runner.SandboxID, error) {
	return runner.SandboxID(spec.AttemptID), nil
}

func (f *fakeRunner) Destroy(_ context.Context, _ runner.SandboxID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.destroyed++
	return nil
}

func (f *fakeRunner) GC(_ context.Context) error { return nil }

func (f *fakeRunner) OpenTerminal(_ context.Context, _ runner.SandboxID, _ string) (runner.PTY, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeRunner) Health(_ context.Context) runner.Health {
	return runner.Health{OK: true}
}
