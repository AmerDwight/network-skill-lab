package docker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

func (p *Provider) Exec(ctx context.Context, sb runner.SandboxID, node string, cmd []string, opts runner.ExecOptions) (runner.ExecResult, error) {
	name := containerName(string(sb), node)
	inspected, err := p.cli.ContainerInspect(ctx, name)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return runner.ExecResult{}, p.missing(ctx, sb, node)
		}
		return runner.ExecResult{}, fmt.Errorf("inspect %s: %w", name, err)
	}
	if inspected.Config != nil {
		opts.Env = execEnv(inspected.Config.Labels[labelRole], opts.Env)
	}
	return p.exec(ctx, name, cmd, opts)
}

func execEnv(role string, env map[string]string) map[string]string {
	if role != runner.RoleK3sServer {
		return env
	}
	out := maps.Clone(env)
	if out == nil {
		out = map[string]string{}
	}
	out["KUBECONFIG"] = kubeconfigPath
	return out
}

func (p *Provider) missing(ctx context.Context, sb runner.SandboxID, node string) error {
	containers, err := p.cli.ContainerList(ctx, container.ListOptions{All: true, Filters: attemptFilter(string(sb))})
	if err != nil {
		return fmt.Errorf("list containers of sandbox %s: %w", sb, err)
	}
	if len(containers) == 0 {
		return fmt.Errorf("sandbox %s: %w", sb, runner.ErrSandboxNotFound)
	}
	return fmt.Errorf("sandbox %s node %s: %w", sb, node, runner.ErrNodeNotFound)
}

func (p *Provider) exec(ctx context.Context, name string, cmd []string, opts runner.ExecOptions) (runner.ExecResult, error) {
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	user := opts.User
	if user == "" {
		user = "root"
	}
	created, err := p.cli.ContainerExecCreate(ctx, name, container.ExecOptions{
		User:         user,
		AttachStdin:  opts.Stdin != nil,
		AttachStdout: true,
		AttachStderr: true,
		Env:          envSlice(opts.Env),
		Cmd:          cmd,
	})
	if err != nil {
		return runner.ExecResult{}, fmt.Errorf("create exec on %s: %w", name, err)
	}

	attached, err := p.cli.ContainerExecAttach(ctx, created.ID, container.ExecAttachOptions{})
	if err != nil {
		return runner.ExecResult{}, fmt.Errorf("attach exec on %s: %w", name, err)
	}
	defer attached.Close()

	if opts.Stdin != nil {
		go func() {
			_, _ = io.Copy(attached.Conn, opts.Stdin)
			_ = attached.CloseWrite()
		}()
	}

	var stdout, stderr bytes.Buffer
	copied := make(chan error, 1)
	go func() {
		_, err := stdcopy.StdCopy(&stdout, &stderr, attached.Reader)
		copied <- err
	}()

	var timedOut bool
	select {
	case err := <-copied:
		if err != nil {
			return runner.ExecResult{}, fmt.Errorf("read exec output on %s: %w", name, err)
		}
	case <-ctx.Done():
		timedOut = opts.Timeout > 0 && errors.Is(ctx.Err(), context.DeadlineExceeded)
		attached.Close()
		<-copied
	}

	result := runner.ExecResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), TimedOut: timedOut}
	inspected, err := p.cli.ContainerExecInspect(context.WithoutCancel(ctx), created.ID)
	if err != nil {
		return result, fmt.Errorf("inspect exec on %s: %w", name, err)
	}
	if inspected.Running {
		result.ExitCode = -1
	} else {
		result.ExitCode = inspected.ExitCode
	}
	return result, nil
}

func envSlice(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for _, key := range slices.Sorted(maps.Keys(env)) {
		out = append(out, key+"="+env[key])
	}
	return out
}
