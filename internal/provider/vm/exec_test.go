package vm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
)

func b64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

type agentStub struct {
	statusPolls atomic.Int32
	pending     int32
	final       string
	started     guestExecArgs
}

func (a *agentStub) handle(args []string) ([]byte, error) {
	if args[0] != "qemu-agent-command" {
		return nil, fmt.Errorf("unexpected virsh %v", args)
	}
	var req struct {
		Execute   string          `json:"execute"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(args[2]), &req); err != nil {
		return nil, err
	}
	switch req.Execute {
	case "guest-exec":
		if err := json.Unmarshal(req.Arguments, &a.started); err != nil {
			return nil, err
		}
		return []byte(`{"return":{"pid":42}}`), nil
	case "guest-exec-status":
		var status guestPID
		if err := json.Unmarshal(req.Arguments, &status); err != nil || status.PID != 42 {
			return nil, fmt.Errorf("bad status request %s", req.Arguments)
		}
		if a.statusPolls.Add(1) <= a.pending {
			return []byte(`{"return":{"exited":false}}`), nil
		}
		return []byte(a.final), nil
	}
	return nil, fmt.Errorf("unexpected agent command %s", req.Execute)
}

func TestExecRoundTrip(t *testing.T) {
	fastPolling(t)
	stub := &agentStub{pending: 3, final: fmt.Sprintf(`{"return":{"exited":true,"exitcode":3,"out-data":%q,"err-data":%q}}`, b64("hello\n"), b64("oops\n"))}
	p := newTestProvider(t, &fakeVirsh{handle: stub.handle})

	result, err := p.Exec(t.Context(), "att1", "web01", []string{"bash", "-s"}, runner.ExecOptions{
		Env:   map[string]string{"NSL_IFACE": "eth1"},
		Stdin: strings.NewReader("ip -br addr\n"),
	})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if string(result.Stdout) != "hello\n" || string(result.Stderr) != "oops\n" || result.ExitCode != 3 || result.TimedOut {
		t.Errorf("result = %+v", result)
	}
	if got := stub.statusPolls.Load(); got != 4 {
		t.Errorf("status polled %d times, want 4", got)
	}
	if stub.started.Path != envPath || !slices.Equal(stub.started.Arg, []string{"bash", "-s"}) || !stub.started.CaptureOutput {
		t.Errorf("guest-exec args = %+v", stub.started)
	}
	if stub.started.InputData != b64("ip -br addr\n") {
		t.Errorf("input-data = %q", stub.started.InputData)
	}
	if !slices.Contains(stub.started.Env, "NSL_IFACE=eth1") || !slices.ContainsFunc(stub.started.Env, func(e string) bool { return strings.HasPrefix(e, "PATH=/") }) {
		t.Errorf("env = %v, want the caller's env plus PATH", stub.started.Env)
	}
}

func TestExecTimeoutAndUser(t *testing.T) {
	fastPolling(t)
	stub := &agentStub{final: `{"return":{"exited":true,"exitcode":124}}`}
	p := newTestProvider(t, &fakeVirsh{handle: stub.handle})

	result, err := p.Exec(t.Context(), "att1", "web01", []string{"sleep", "9"}, runner.ExecOptions{Timeout: 1500 * time.Millisecond, User: "nsl"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.TimedOut || result.ExitCode != 124 {
		t.Errorf("result = %+v, want a timeout", result)
	}
	want := []string{"timeout", "1.5s", "runuser", "-u", "nsl", "--", "sleep", "9"}
	if !slices.Equal(stub.started.Arg, want) {
		t.Errorf("argv = %v, want %v", stub.started.Arg, want)
	}
	if stub.started.InputData != "" {
		t.Errorf("input-data = %q without stdin", stub.started.InputData)
	}
}

func TestExecExitCode124WithoutTimeoutIsNotATimeout(t *testing.T) {
	fastPolling(t)
	stub := &agentStub{final: `{"return":{"exited":true,"exitcode":124}}`}
	p := newTestProvider(t, &fakeVirsh{handle: stub.handle})
	result, err := p.Exec(t.Context(), "att1", "web01", []string{"false"}, runner.ExecOptions{})
	if err != nil || result.TimedOut {
		t.Errorf("result = %+v, %v", result, err)
	}
}

func TestExecKilledBySignal(t *testing.T) {
	fastPolling(t)
	stub := &agentStub{final: `{"return":{"exited":true,"signal":9}}`}
	p := newTestProvider(t, &fakeVirsh{handle: stub.handle})
	result, err := p.Exec(t.Context(), "att1", "web01", []string{"true"}, runner.ExecOptions{})
	if err != nil || result.ExitCode != 137 {
		t.Errorf("result = %+v, %v, want exit code 137", result, err)
	}
}

func TestExecHonorsCancellation(t *testing.T) {
	fastPolling(t)
	stub := &agentStub{pending: 1 << 30}
	p := newTestProvider(t, &fakeVirsh{handle: stub.handle})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err := p.Exec(ctx, "att1", "web01", []string{"sleep", "inf"}, runner.ExecOptions{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Exec() error = %v, want the context error", err)
	}
}

func TestExecReportsMissingSandboxAndNode(t *testing.T) {
	cases := map[string]struct {
		domains map[string]owner
		want    error
	}{
		"no sandbox": {domains: map[string]owner{"nsl-inst1-other-web01": {Instance: "inst1", Attempt: "other"}}, want: runner.ErrSandboxNotFound},
		"no node":    {domains: map[string]owner{"nsl-inst1-att1-db01": {Instance: "inst1", Attempt: "att1"}}, want: runner.ErrNodeNotFound},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			state := &libvirtState{domains: tc.domains, networks: map[string]owner{}, running: map[string]bool{}}
			v := &fakeVirsh{handle: func(args []string) ([]byte, error) {
				if args[0] == "qemu-agent-command" || args[0] == "domifaddr" {
					return nil, domainNotFound(args[1])
				}
				return state.handle(args)
			}}
			p := newTestProvider(t, v)
			if _, err := p.Exec(t.Context(), "att1", "web01", []string{"true"}, runner.ExecOptions{}); !errors.Is(err, tc.want) {
				t.Errorf("Exec() error = %v, want %v", err, tc.want)
			}
			if _, err := p.OpenTerminal(t.Context(), "att1", "web01"); !errors.Is(err, tc.want) {
				t.Errorf("OpenTerminal() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestAgentCommandEncoding(t *testing.T) {
	var seen []string
	v := &fakeVirsh{handle: func(args []string) ([]byte, error) {
		seen = args
		return []byte(`{"return":{}}` + "\n"), nil
	}}
	if err := agentPing(t.Context(), v, "dom1"); err != nil {
		t.Fatal(err)
	}
	want := []string{"qemu-agent-command", "dom1", `{"execute":"guest-ping"}`, "--timeout", "2"}
	if !slices.Equal(seen, want) {
		t.Errorf("args = %q, want %q", seen, want)
	}

	v.handle = func([]string) ([]byte, error) { return []byte("garbage"), nil }
	if err := agentPing(t.Context(), v, "dom1"); err == nil {
		t.Error("agentPing accepted a reply that is not JSON")
	}
}

func TestAddressParsing(t *testing.T) {
	lease := ` Name       MAC address          Protocol     Address
-------------------------------------------------------------------------------
 vnet3      52:54:00:aa:bb:cc    ipv4         192.168.122.57/24
`
	if got := leaseAddress([]byte(lease)); got != "192.168.122.57" {
		t.Errorf("leaseAddress = %q", got)
	}
	agent := ` Name       MAC address          Protocol     Address
-------------------------------------------------------------------------------
 lo         00:00:00:00:00:00    ipv4         127.0.0.1/8
 -          -                    ipv6         ::1/128
 eth0       52:54:00:aa:bb:cc    ipv4         192.168.122.57/24
 eth1       52:54:00:aa:bb:dd    ipv4         10.0.5.10/24
`
	if got := agentAddress([]byte(agent)); got != "192.168.122.57" {
		t.Errorf("agentAddress = %q", got)
	}
	if got := leaseAddress([]byte(" Name MAC address Protocol Address\n----\n")); got != "" {
		t.Errorf("leaseAddress without a lease = %q", got)
	}
}
