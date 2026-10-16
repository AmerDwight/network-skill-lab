package runnerproto

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
)

func TestMessageRoundTrip(t *testing.T) {
	messages := []Message{
		Hello{Proto: Version, Name: "lab-01", Version: "v1", Capabilities: []string{"container"}, Capacity: 3, Sandboxes: []string{"a", "b"}},
		Welcome{RunnerID: "01J"},
		Ping{Active: 2},
		Ping{},
		Call{ID: 7, Method: MethodExec, Params: json.RawMessage(`{"sandbox":"s"}`)},
		Call{ID: 8, Method: MethodGC},
		Result{ID: 7, OK: true, Value: json.RawMessage(`{"stream":1}`)},
		Result{ID: 9, Error: "boom", Code: CodeSandboxNotFound},
		Progress{ID: 7, Step: "network"},
		StreamClosed{Stream: 3},
		Cancel{ID: 7},
	}
	for _, m := range messages {
		t.Run(fmt.Sprintf("%T", m), func(t *testing.T) {
			data, err := Encode(m)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			var envelope struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(data, &envelope); err != nil || envelope.Type != m.messageType() {
				t.Fatalf("encoded %s has type %q (%v)", data, envelope.Type, err)
			}
			got, err := Decode(data)
			if err != nil {
				t.Fatalf("Decode(%s) error = %v", data, err)
			}
			if !reflect.DeepEqual(got, m) {
				t.Errorf("Decode(%s) = %#v, want %#v", data, got, m)
			}
		})
	}
}

func TestHelloWireFormat(t *testing.T) {
	data, err := Encode(Hello{Proto: 1, Name: "lab-01", Version: "v1", Capabilities: []string{"container"}, Capacity: 3})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"hello","proto":1,"name":"lab-01","version":"v1","capabilities":["container"],"capacity":3}`
	if string(data) != want {
		t.Errorf("Encode() = %s, want %s", data, want)
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	for _, input := range []string{`not json`, `{"type":"nope"}`, `{}`, `{"type":"ping","active":"x"}`} {
		if _, err := Decode([]byte(input)); err == nil {
			t.Errorf("Decode(%s) succeeded", input)
		}
	}
}

func TestStreamFrameRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		id   uint32
		data []byte
	}{{1, []byte("hello")}, {0xdeadbeef, nil}, {0, []byte{0, 1, 2}}} {
		frame := EncodeStreamFrame(tc.id, tc.data)
		if len(frame) != 4+len(tc.data) {
			t.Fatalf("frame length = %d", len(frame))
		}
		id, data, err := DecodeStreamFrame(frame)
		if err != nil || id != tc.id || !bytes.Equal(data, tc.data) {
			t.Errorf("DecodeStreamFrame() = %d, %q, %v; want %d, %q", id, data, err, tc.id, tc.data)
		}
	}
	if !bytes.Equal(EncodeStreamFrame(0x01020304, nil), []byte{1, 2, 3, 4}) {
		t.Error("stream id is not big-endian")
	}
	if _, _, err := DecodeStreamFrame([]byte{1, 2, 3}); !errors.Is(err, ErrShortFrame) {
		t.Errorf("short frame error = %v", err)
	}
}

func TestSpecRoundTrip(t *testing.T) {
	spec := runner.SandboxSpec{
		AttemptID: "att1",
		Image:     "nsl/node",
		Nodes: []runner.NodeSpec{
			{Name: "k3s01", Role: "k3s-server", K3sDisable: []string{}},
			{Name: "web01", Role: "ubuntu"},
		},
		Links: []runner.LinkSpec{{Name: "l1", Subnet: "10.0.5.0/24", Endpoints: []runner.EndpointSpec{
			{Node: "web01", Iface: "eth1", Address: "10.0.5.10/24"},
		}}},
		Setup:    runner.Script{Name: "setup.sh", Content: []byte("#!/bin/bash\n\x00\xff")},
		Env:      map[string]string{"NSL_X": "1"},
		Internet: true,
	}
	data, err := json.Marshal(FromSpec(spec))
	if err != nil {
		t.Fatal(err)
	}
	var params ProvisionParams
	if err := json.Unmarshal(data, &params); err != nil {
		t.Fatal(err)
	}
	if got := params.Spec(); !reflect.DeepEqual(got, spec) {
		t.Errorf("round trip = %#v, want %#v", got, spec)
	}
}

func TestExecRoundTrip(t *testing.T) {
	opts := runner.ExecOptions{Env: map[string]string{"A": "b"}, Stdin: strings.NewReader("script\x00"), Timeout: 1500 * time.Millisecond, User: "nsl"}
	params, err := FromExec("sb", "web01", []string{"bash", "-s"}, opts)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"stdin":"`) {
		t.Errorf("stdin is not base64 in %s", data)
	}
	var decoded ExecParams
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	got := decoded.Options()
	stdin, _ := io.ReadAll(got.Stdin)
	if string(stdin) != "script\x00" || got.Timeout != opts.Timeout || got.User != "nsl" || got.Env["A"] != "b" {
		t.Errorf("Options() = %+v stdin %q", got, stdin)
	}
	if decoded.Sandbox != "sb" || decoded.Node != "web01" || len(decoded.Cmd) != 2 {
		t.Errorf("decoded = %+v", decoded)
	}

	noStdin, err := FromExec("sb", "web01", []string{"true"}, runner.ExecOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if noStdin.Options().Stdin != nil {
		t.Error("an exec without stdin gained one")
	}

	result := runner.ExecResult{Stdout: []byte("out\xff"), Stderr: []byte("err"), ExitCode: 3, TimedOut: true}
	data, err = json.Marshal(FromExecResult(result))
	if err != nil {
		t.Fatal(err)
	}
	var wire ExecResult
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(wire.Runner(), result) {
		t.Errorf("result round trip = %+v", wire.Runner())
	}
}

func TestResultErrors(t *testing.T) {
	for _, sentinel := range []error{runner.ErrSandboxNotFound, runner.ErrNodeNotFound} {
		result, err := NewResult(4, nil, fmt.Errorf("sandbox x: %w", sentinel))
		if err != nil {
			t.Fatal(err)
		}
		if result.OK {
			t.Fatal("a failed result is ok")
		}
		if got := result.Err(); !errors.Is(got, sentinel) || got.Error() != "sandbox x: "+sentinel.Error() {
			t.Errorf("Err() = %v, want wrapping %v", got, sentinel)
		}
	}

	plain, _ := NewResult(5, nil, errors.New("docker down"))
	if err := plain.Err(); err == nil || errors.Is(err, runner.ErrSandboxNotFound) || plain.Code != "" {
		t.Errorf("plain error = %v code %q", err, plain.Code)
	}

	ok, err := NewResult(6, StreamResult{Stream: 2}, nil)
	if err != nil || !ok.OK || ok.Err() != nil || string(ok.Value) != `{"stream":2}` {
		t.Errorf("ok result = %+v, %v", ok, err)
	}
}

func TestHealthAndCapabilities(t *testing.T) {
	health := runner.Health{OK: true, Docker: true, Image: true, ImageName: "nsl/node", Instance: "lab-01", MemAvailableMB: 512, Error: ""}
	if FromHealth(health).Runner() != health {
		t.Error("health round trip changed the value")
	}
	envs := []runner.Env{runner.EnvContainer, runner.EnvVM}
	if !reflect.DeepEqual(Capabilities(CapabilityNames(envs)), envs) {
		t.Error("capabilities round trip changed the value")
	}
}
