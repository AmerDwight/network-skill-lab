package docker

import (
	"context"
	"fmt"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

func (p *Provider) OpenTerminal(ctx context.Context, sb runner.SandboxID, node string) (runner.PTY, error) {
	name := containerName(string(sb), node)
	created, err := p.cli.ContainerExecCreate(ctx, name, container.ExecOptions{
		User:         "nsl",
		WorkingDir:   "/home/nsl",
		Tty:          true,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Env:          []string{"TERM=xterm-256color"},
		Cmd:          []string{"bash", "-l"},
	})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil, p.missing(ctx, sb, node)
		}
		return nil, fmt.Errorf("create terminal exec on %s: %w", name, err)
	}

	attached, err := p.cli.ContainerExecAttach(ctx, created.ID, container.ExecAttachOptions{Tty: true})
	if err != nil {
		return nil, fmt.Errorf("attach terminal exec on %s: %w", name, err)
	}
	p.log(string(sb)).Info("opened terminal", "node", node, "exec", created.ID)
	return &pty{cli: p.cli, execID: created.ID, attached: attached, ctx: context.WithoutCancel(ctx)}, nil
}

type pty struct {
	cli      client.APIClient
	execID   string
	attached types.HijackedResponse
	ctx      context.Context
}

func (t *pty) Read(b []byte) (int, error) {
	return t.attached.Reader.Read(b)
}

func (t *pty) Write(b []byte) (int, error) {
	return t.attached.Conn.Write(b)
}

func (t *pty) Close() error {
	t.attached.Close()
	return nil
}

func (t *pty) Resize(cols, rows uint16) error {
	opts := container.ResizeOptions{Width: uint(cols), Height: uint(rows)}
	if err := t.cli.ContainerExecResize(t.ctx, t.execID, opts); err != nil {
		return fmt.Errorf("resize terminal exec %s: %w", t.execID, err)
	}
	return nil
}
