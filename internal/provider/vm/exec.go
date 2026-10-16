package vm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
)

const (
	envPath         = "/usr/bin/env"
	timeoutExitCode = 124
	hostTimeoutPad  = 10 * time.Second
)

var baseEnv = map[string]string{
	"PATH":            "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	"HOME":            "/root",
	"LANG":            "C.UTF-8",
	"DEBIAN_FRONTEND": "noninteractive",
}

func (p *Provider) Exec(ctx context.Context, sb runner.SandboxID, node string, cmd []string, opts runner.ExecOptions) (runner.ExecResult, error) {
	name := domainName(p.cfg.Instance, string(sb), node)
	result, err := p.exec(ctx, name, cmd, opts)
	if isDomainNotFound(err) {
		return runner.ExecResult{}, p.missing(ctx, sb, node)
	}
	return result, err
}

func (p *Provider) missing(ctx context.Context, sb runner.SandboxID, node string) error {
	inv, err := p.owned(ctx, p.sandboxMatch(string(sb)))
	if err != nil {
		return fmt.Errorf("look up sandbox %s: %w", sb, err)
	}
	if len(inv.domains) == 0 {
		return fmt.Errorf("sandbox %s: %w", sb, runner.ErrSandboxNotFound)
	}
	return fmt.Errorf("sandbox %s node %s: %w", sb, node, runner.ErrNodeNotFound)
}

func execArgv(cmd []string, opts runner.ExecOptions) []string {
	argv := []string{envPath}
	if opts.Timeout > 0 {
		argv = append(argv, "timeout", strconv.FormatFloat(opts.Timeout.Seconds(), 'f', -1, 64)+"s")
	}
	if opts.User != "" && opts.User != "root" {
		argv = append(argv, "runuser", "-u", opts.User, "--")
	}
	return append(argv, cmd...)
}

func execEnv(env map[string]string) []string {
	merged := maps.Clone(baseEnv)
	maps.Copy(merged, env)
	out := make([]string, 0, len(merged))
	for _, key := range slices.Sorted(maps.Keys(merged)) {
		out = append(out, key+"="+merged[key])
	}
	return out
}

func (p *Provider) exec(ctx context.Context, domain string, cmd []string, opts runner.ExecOptions) (runner.ExecResult, error) {
	if len(cmd) == 0 {
		return runner.ExecResult{}, errors.New("exec: empty command")
	}
	var stdin []byte
	if opts.Stdin != nil {
		var err error
		if stdin, err = io.ReadAll(opts.Stdin); err != nil {
			return runner.ExecResult{}, fmt.Errorf("read stdin: %w", err)
		}
	}
	execCtx := ctx
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, opts.Timeout+hostTimeoutPad)
		defer cancel()
	}

	out, err := guestExec(execCtx, p.virsh, domain, execArgv(cmd, opts), execEnv(opts.Env), stdin)
	if err != nil {
		if ctx.Err() == nil && errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			return runner.ExecResult{ExitCode: -1, TimedOut: true}, nil
		}
		if ctx.Err() != nil {
			return runner.ExecResult{}, ctx.Err()
		}
		return runner.ExecResult{}, fmt.Errorf("exec on %s: %w", domain, err)
	}
	return runner.ExecResult{
		Stdout:   out.stdout,
		Stderr:   out.stderr,
		ExitCode: out.exitCode,
		TimedOut: opts.Timeout > 0 && out.exitCode == timeoutExitCode,
	}, nil
}
