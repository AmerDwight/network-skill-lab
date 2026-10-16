package attempt

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

var errNotPlaced = errors.New("provision goes through the attempt service")

type RunnerStatus struct {
	runner.Member
	Active int
}

func (s *Service) RunnerStatuses(ctx context.Context) ([]RunnerStatus, error) {
	active, err := s.store.Attempts.CountActiveByRunner(ctx)
	if err != nil {
		return nil, err
	}
	members := s.runners.Members()
	statuses := make([]RunnerStatus, 0, len(members))
	for _, m := range members {
		statuses = append(statuses, RunnerStatus{Member: m, Active: active[m.Name]})
	}
	return statuses, nil
}

// RunnerDisconnected ends every attempt still placed on the runner, because its
// terminals and checks are gone with the connection.
func (s *Service) RunnerDisconnected(name string) {
	if s.stopping() {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
		attempts, err := s.store.Attempts.ListNonTerminal(ctx)
		cancel()
		if err != nil {
			s.log.Error("list attempts of a disconnected runner", "runner", name, "error", err)
			return
		}
		ended := 0
		for _, att := range attempts {
			if runnerOf(att) != name {
				continue
			}
			if s.failDisconnected(att.ID) {
				ended++
			}
		}
		s.log.Warn("runner disconnected", "runner", name, "attempts_ended", ended)
	}()
}

func (s *Service) failDisconnected(id string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	_, err := s.end(ctx, id, store.StatusError, ReasonRunnerDisconnected, store.StatusProvisioning, store.StatusRunning)
	if err != nil && !errors.Is(err, ErrTerminal) {
		s.log.Error("end attempt of a disconnected runner", "attempt", id, "error", err)
	}
	return err == nil
}

// RunnerReconnected destroys the sandboxes a runner kept through a disconnect; their
// attempts already ended when the connection dropped.
func (s *Service) RunnerReconnected(name string, sandboxes []string) {
	if s.stopping() || len(sandboxes) == 0 {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		r, ok := s.runners.Get(name)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), destroyTimeout)
		defer cancel()
		for _, sb := range sandboxes {
			err := r.Destroy(ctx, runner.SandboxID(sb))
			if err != nil && !errors.Is(err, runner.ErrSandboxNotFound) {
				s.log.Error("destroy a sandbox left from before the disconnect", "runner", name, "sandbox", sb, "error", err)
				continue
			}
			s.log.Info("destroyed a sandbox left from before the disconnect", "runner", name, "sandbox", sb)
		}
	}()
}

func (s *Service) bind(sb runner.SandboxID, name string) {
	s.sandboxMu.Lock()
	defer s.sandboxMu.Unlock()
	s.sandboxes[sb] = name
}

func (s *Service) unbind(sb runner.SandboxID) {
	s.sandboxMu.Lock()
	defer s.sandboxMu.Unlock()
	delete(s.sandboxes, sb)
}

func (s *Service) runnerFor(sb runner.SandboxID) (runner.Runner, error) {
	s.sandboxMu.Lock()
	name, ok := s.sandboxes[sb]
	s.sandboxMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", runner.ErrSandboxNotFound, sb)
	}
	r, ok := s.runners.Get(name)
	if !ok {
		return nil, fmt.Errorf("sandbox %s: runner %s: %s", sb, name, ReasonRunnerDisconnected)
	}
	return r, nil
}

// Runner returns a runner.Runner that sends each sandbox call to the runner the
// sandbox was placed on, for the checker, the recorder and the terminals.
func (s *Service) Runner() runner.Runner {
	return routed{s: s}
}

type routed struct {
	s *Service
}

func (r routed) Capabilities() []runner.Env {
	var envs []runner.Env
	for _, m := range r.s.runners.Members() {
		if !m.Connected {
			continue
		}
		for _, env := range m.Capabilities {
			if !slices.Contains(envs, env) {
				envs = append(envs, env)
			}
		}
	}
	return envs
}

func (routed) Provision(context.Context, runner.SandboxSpec) (runner.SandboxID, error) {
	return "", errNotPlaced
}

func (r routed) OpenTerminal(ctx context.Context, sb runner.SandboxID, node string) (runner.PTY, error) {
	target, err := r.s.runnerFor(sb)
	if err != nil {
		return nil, err
	}
	return target.OpenTerminal(ctx, sb, node)
}

func (r routed) Exec(ctx context.Context, sb runner.SandboxID, node string, cmd []string, opts runner.ExecOptions) (runner.ExecResult, error) {
	target, err := r.s.runnerFor(sb)
	if err != nil {
		return runner.ExecResult{}, err
	}
	return target.Exec(ctx, sb, node, cmd, opts)
}

func (r routed) Destroy(ctx context.Context, sb runner.SandboxID) error {
	target, err := r.s.runnerFor(sb)
	if err != nil {
		return err
	}
	return target.Destroy(ctx, sb)
}

func (r routed) GC(ctx context.Context) error {
	var errs []error
	for _, m := range r.s.runners.Members() {
		target, ok := r.s.runners.Get(m.Name)
		if !ok {
			continue
		}
		if err := target.GC(ctx); err != nil {
			errs = append(errs, fmt.Errorf("runner %s: %w", m.Name, err))
		}
	}
	return errors.Join(errs...)
}

func (r routed) Health(ctx context.Context) runner.Health {
	if local, ok := r.s.runners.Local(); ok {
		return local.Health(ctx)
	}
	connected := len(r.Capabilities()) > 0
	health := runner.Health{OK: connected, Error: "the in-process runner is disabled because Docker is not reachable on this host"}
	if !connected {
		health.Error += " and no remote runner is connected"
	}
	return health
}
