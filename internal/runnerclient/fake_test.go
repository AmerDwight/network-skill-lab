package runnerclient

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
)

type fakeProvider struct {
	mu        sync.Mutex
	sandboxes map[runner.SandboxID]bool
	destroyed []runner.SandboxID
	ptys      []*fakePTY
	block     chan struct{}
	canceled  chan struct{}
	gcCalls   int
	gcErr     error
	onGC      func()
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{sandboxes: map[runner.SandboxID]bool{}, canceled: make(chan struct{}, 8)}
}

func (f *fakeProvider) Capabilities() []runner.Env { return []runner.Env{runner.EnvContainer} }

func (f *fakeProvider) Provision(_ context.Context, spec runner.SandboxSpec) (runner.SandboxID, error) {
	for _, step := range []string{"network", "nodes", "setup"} {
		spec.Progress(step)
	}
	sb := runner.SandboxID(spec.AttemptID)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sandboxes[sb] = true
	return sb, nil
}

func (f *fakeProvider) has(sb runner.SandboxID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sandboxes[sb]
}

func (f *fakeProvider) Exec(ctx context.Context, sb runner.SandboxID, node string, cmd []string, opts runner.ExecOptions) (runner.ExecResult, error) {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			f.canceled <- struct{}{}
			return runner.ExecResult{}, ctx.Err()
		}
	}
	if !f.has(sb) {
		return runner.ExecResult{}, fmt.Errorf("sandbox %s: %w", sb, runner.ErrSandboxNotFound)
	}
	var stdin []byte
	if opts.Stdin != nil {
		stdin, _ = io.ReadAll(opts.Stdin)
	}
	stdout := fmt.Sprintf("%s|%s|%s|%s", node, strings.Join(cmd, " "), stdin, opts.Env["NSL_X"])
	return runner.ExecResult{Stdout: []byte(stdout), Stderr: []byte("warn\xff"), ExitCode: 3}, nil
}

func (f *fakeProvider) OpenTerminal(_ context.Context, sb runner.SandboxID, node string) (runner.PTY, error) {
	if !f.has(sb) {
		return nil, fmt.Errorf("sandbox %s: %w", sb, runner.ErrSandboxNotFound)
	}
	pty := newFakePTY()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ptys = append(f.ptys, pty)
	return pty, nil
}

func (f *fakeProvider) pty(i int) *fakePTY {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i >= len(f.ptys) {
		return nil
	}
	return f.ptys[i]
}

func (f *fakeProvider) Destroy(_ context.Context, sb runner.SandboxID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sandboxes, sb)
	f.destroyed = append(f.destroyed, sb)
	return nil
}

func (f *fakeProvider) destroyedList() []runner.SandboxID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]runner.SandboxID(nil), f.destroyed...)
}

func (f *fakeProvider) GC(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gcCalls++
	if f.onGC != nil {
		f.onGC()
	}
	if f.gcErr != nil {
		return f.gcErr
	}
	clear(f.sandboxes)
	return nil
}

func (f *fakeProvider) gcCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gcCalls
}

func (f *fakeProvider) Health(context.Context) runner.Health {
	return runner.Health{OK: true, Docker: true, Image: true, ImageName: "nsl/node", Instance: "fake", MemAvailableMB: 1024}
}

type fakePTY struct {
	output chan []byte
	done   chan struct{}
	once   sync.Once

	mu   sync.Mutex
	size [2]uint16
}

func newFakePTY() *fakePTY {
	return &fakePTY{output: make(chan []byte, 64), done: make(chan struct{})}
}

func (p *fakePTY) Read(b []byte) (int, error) {
	select {
	case data := <-p.output:
		return copy(b, data), nil
	case <-p.done:
		return 0, io.EOF
	}
}

func (p *fakePTY) Write(b []byte) (int, error) {
	select {
	case p.output <- append([]byte("echo:"), b...):
		return len(b), nil
	case <-p.done:
		return 0, io.ErrClosedPipe
	}
}

func (p *fakePTY) Close() error {
	p.once.Do(func() { close(p.done) })
	return nil
}

func (p *fakePTY) exit() { _ = p.Close() }

func (p *fakePTY) closed() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

func (p *fakePTY) Resize(cols, rows uint16) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.size = [2]uint16{cols, rows}
	return nil
}

func (p *fakePTY) dims() [2]uint16 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.size
}
