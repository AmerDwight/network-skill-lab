package runnerserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/runnerproto"
)

type RemoteRunner struct {
	c *conn
}

var _ runner.Runner = (*RemoteRunner)(nil)

func (r *RemoteRunner) Capabilities() []runner.Env {
	return runnerproto.Capabilities(r.c.hello.Capabilities)
}

func (r *RemoteRunner) Provision(ctx context.Context, spec runner.SandboxSpec) (runner.SandboxID, error) {
	value, err := r.c.call(ctx, runnerproto.MethodProvision, runnerproto.FromSpec(spec), spec.Progress)
	if err != nil {
		return "", err
	}
	var result runnerproto.SandboxResult
	if err := decode(runnerproto.MethodProvision, value, &result); err != nil {
		return "", err
	}
	return runner.SandboxID(result.Sandbox), nil
}

func (r *RemoteRunner) OpenTerminal(ctx context.Context, sb runner.SandboxID, node string) (runner.PTY, error) {
	reply, err := r.c.roundTrip(ctx, runnerproto.MethodOpenTerminal, runnerproto.OpenTerminalParams{Sandbox: string(sb), Node: node}, nil)
	if err != nil {
		return nil, err
	}
	if reply.stream == nil {
		return nil, errors.New("open_terminal: the runner returned no stream")
	}
	return reply.stream, nil
}

func (r *RemoteRunner) Exec(ctx context.Context, sb runner.SandboxID, node string, cmd []string, opts runner.ExecOptions) (runner.ExecResult, error) {
	params, err := runnerproto.FromExec(sb, node, cmd, opts)
	if err != nil {
		return runner.ExecResult{}, err
	}
	value, err := r.c.call(ctx, runnerproto.MethodExec, params, nil)
	if err != nil {
		return runner.ExecResult{}, err
	}
	var result runnerproto.ExecResult
	if err := decode(runnerproto.MethodExec, value, &result); err != nil {
		return runner.ExecResult{}, err
	}
	return result.Runner(), nil
}

func (r *RemoteRunner) Destroy(ctx context.Context, sb runner.SandboxID) error {
	_, err := r.c.call(ctx, runnerproto.MethodDestroy, runnerproto.SandboxParams{Sandbox: string(sb)}, nil)
	return err
}

func (r *RemoteRunner) GC(ctx context.Context) error {
	_, err := r.c.call(ctx, runnerproto.MethodGC, nil, nil)
	return err
}

func (r *RemoteRunner) Health(ctx context.Context) runner.Health {
	value, err := r.c.call(ctx, runnerproto.MethodHealth, nil, nil)
	if err != nil {
		return runner.Health{Instance: r.c.name, Error: err.Error()}
	}
	var health runnerproto.Health
	if err := decode(runnerproto.MethodHealth, value, &health); err != nil {
		return runner.Health{Instance: r.c.name, Error: err.Error()}
	}
	return health.Runner()
}

func decode(method string, value json.RawMessage, into any) error {
	if err := json.Unmarshal(value, into); err != nil {
		return fmt.Errorf("decode %s result: %w", method, err)
	}
	return nil
}
