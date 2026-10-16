package vm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	pollStart = 50 * time.Millisecond
	pollMax   = 500 * time.Millisecond
)

type agentRequest struct {
	Execute   string `json:"execute"`
	Arguments any    `json:"arguments,omitempty"`
}

type guestExecArgs struct {
	Path          string   `json:"path"`
	Arg           []string `json:"arg,omitempty"`
	Env           []string `json:"env,omitempty"`
	InputData     string   `json:"input-data,omitempty"`
	CaptureOutput bool     `json:"capture-output"`
}

type guestPID struct {
	PID int `json:"pid"`
}

type guestExecStatus struct {
	Exited   bool   `json:"exited"`
	ExitCode *int   `json:"exitcode"`
	Signal   *int   `json:"signal"`
	OutData  string `json:"out-data"`
	ErrData  string `json:"err-data"`
}

type guestOutput struct {
	stdout   []byte
	stderr   []byte
	exitCode int
}

func (s guestExecStatus) output() (guestOutput, error) {
	stdout, err := base64.StdEncoding.DecodeString(s.OutData)
	if err != nil {
		return guestOutput{}, fmt.Errorf("decode out-data: %w", err)
	}
	stderr, err := base64.StdEncoding.DecodeString(s.ErrData)
	if err != nil {
		return guestOutput{}, fmt.Errorf("decode err-data: %w", err)
	}
	out := guestOutput{stdout: stdout, stderr: stderr, exitCode: -1}
	switch {
	case s.ExitCode != nil:
		out.exitCode = *s.ExitCode
	case s.Signal != nil:
		out.exitCode = 128 + *s.Signal
	}
	return out, nil
}

func agentCommand(ctx context.Context, v virsh, domain string, req agentRequest, result any, extra ...string) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode %s: %w", req.Execute, err)
	}
	args := append([]string{"qemu-agent-command", domain, string(body)}, extra...)
	out, err := v.run(ctx, args...)
	if err != nil {
		return err
	}
	var reply struct {
		Return json.RawMessage `json:"return"`
	}
	if err := json.Unmarshal(out, &reply); err != nil {
		return fmt.Errorf("decode %s reply %q: %w", req.Execute, out, err)
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(reply.Return, result); err != nil {
		return fmt.Errorf("decode %s result %s: %w", req.Execute, reply.Return, err)
	}
	return nil
}

func agentPing(ctx context.Context, v virsh, domain string) error {
	return agentCommand(ctx, v, domain, agentRequest{Execute: "guest-ping"}, nil, "--timeout", "2")
}

func guestExec(ctx context.Context, v virsh, domain string, argv, env []string, stdin []byte) (guestOutput, error) {
	if len(argv) == 0 {
		return guestOutput{}, errors.New("empty command")
	}
	args := guestExecArgs{Path: argv[0], Arg: argv[1:], Env: env, CaptureOutput: true}
	if len(stdin) > 0 {
		args.InputData = base64.StdEncoding.EncodeToString(stdin)
	}
	var started guestPID
	if err := agentCommand(ctx, v, domain, agentRequest{Execute: "guest-exec", Arguments: args}, &started); err != nil {
		return guestOutput{}, err
	}

	interval := pollStart
	for {
		var status guestExecStatus
		req := agentRequest{Execute: "guest-exec-status", Arguments: started}
		if err := agentCommand(ctx, v, domain, req, &status); err != nil {
			if ctx.Err() != nil {
				return guestOutput{}, ctx.Err()
			}
			return guestOutput{}, err
		}
		if status.Exited {
			return status.output()
		}
		select {
		case <-ctx.Done():
			return guestOutput{}, ctx.Err()
		case <-time.After(interval):
		}
		interval = min(interval*2, pollMax)
	}
}
