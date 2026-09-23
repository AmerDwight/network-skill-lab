//go:build content_integration

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/api"
	"github.com/AmerDwight/network-skill-lab/internal/attempt"
	"github.com/AmerDwight/network-skill-lab/internal/checker"
	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/provider/docker"
	"github.com/AmerDwight/network-skill-lab/internal/recorder"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/store"
	"github.com/coder/websocket"
	"github.com/docker/docker/client"
)

const (
	k3sNodeImage     = "nsl/node"
	k3sContentDir    = "../../content"
	k3sRunningLimit  = 120 * time.Second
	k3sPassedLimit   = 120 * time.Second
	k3sKubectlLimit  = 30 * time.Second
	k3sServerNodeRef = "k3s01"
)

type k3sStack struct {
	server   *httptest.Server
	attempts *attempt.Service
	provider *docker.Provider
	store    *store.Store
}

func newK3sStack(t *testing.T) *k3sStack {
	t.Helper()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skipf("docker client unavailable: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		t.Skipf("docker daemon unreachable: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	loaded, err := content.LoadAll(k3sContentDir)
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	provider := docker.New(cli, docker.Options{Image: k3sNodeImage})
	attempts := attempt.New(attempt.Deps{
		Store:    st,
		Runner:   provider,
		Content:  loaded,
		Image:    k3sNodeImage,
		RunnerID: "docker",
		Logger:   logger,
	})
	t.Cleanup(attempts.Close)

	checks := checker.New(checker.Deps{Store: st, Runner: provider, Attempts: attempts, Interval: time.Second, Logger: logger})
	t.Cleanup(checks.Close)
	attempts.SetSweeper(checks)
	recordings := recorder.New(recorder.Deps{Store: st, Runner: provider, Attempts: attempts, Interval: time.Second, DataDir: dataDir, Logger: logger})
	t.Cleanup(recordings.Close)

	checks.Start(t.Context())
	recordings.Start(t.Context())

	server := httptest.NewServer(api.New(api.Deps{
		Attempts: attempts,
		Content:  loaded,
		Store:    st,
		Runner:   provider,
		Recorder: recordings,
		Logger:   logger,
	}))
	t.Cleanup(server.Close)

	return &k3sStack{server: server, attempts: attempts, provider: provider, store: st}
}

func (s *k3sStack) request(t *testing.T, method, path, body string) map[string]any {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, s.server.URL+path, reader)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, path, err)
	}
	resp, err := s.server.Client().Do(req)
	if err != nil {
		t.Fatalf("send request %s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body of %s %s: %v", method, path, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode body %q: %v", raw, err)
	}
	decoded["_status"] = float64(resp.StatusCode)
	return decoded
}

func (s *k3sStack) start(t *testing.T, labID string) string {
	t.Helper()
	created := s.request(t, http.MethodPost, "/api/attempts", `{"lab_id":"`+labID+`","mode":"guided"}`)
	if created["_status"] != float64(http.StatusCreated) {
		t.Fatalf("create attempt for %s: %v", labID, created)
	}
	id := created["id"].(string)
	t.Cleanup(func() {
		ctx := context.WithoutCancel(t.Context())
		if _, err := s.attempts.Abandon(ctx, id); err != nil && !errors.Is(err, attempt.ErrTerminal) {
			t.Errorf("cleanup abandon: %v", err)
		}
		if err := s.provider.Destroy(ctx, runner.SandboxID(id)); err != nil {
			t.Logf("cleanup destroy: %v", err)
		}
	})
	return id
}

func (s *k3sStack) events(t *testing.T, id string) <-chan map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(s.server.URL, "http")+"/ws/attempts/"+id+"/events", nil)
	if err != nil {
		t.Fatalf("dial the events socket: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })

	messages := make(chan map[string]any, 256)
	go func() {
		defer close(messages)
		for {
			kind, data, err := conn.Read(context.WithoutCancel(t.Context()))
			if err != nil {
				return
			}
			if kind != websocket.MessageText {
				continue
			}
			var message map[string]any
			if err := json.Unmarshal(data, &message); err != nil {
				return
			}
			messages <- message
		}
	}()
	return messages
}

func (s *k3sStack) params(t *testing.T, id string) map[string]string {
	t.Helper()
	record, err := s.store.Attempts.Get(t.Context(), id)
	if err != nil {
		t.Fatalf("read attempt %s: %v", id, err)
	}
	var params map[string]string
	if err := json.Unmarshal([]byte(record.ParamsJSON), &params); err != nil {
		t.Fatalf("decode params %q: %v", record.ParamsJSON, err)
	}
	return params
}

func (s *k3sStack) kubectl(t *testing.T, id string, args ...string) {
	t.Helper()
	opts := runner.ExecOptions{Timeout: k3sKubectlLimit}
	result, err := s.provider.Exec(t.Context(), runner.SandboxID(id), k3sServerNodeRef, append([]string{"kubectl"}, args...), opts)
	if err != nil {
		t.Fatalf("exec kubectl %v: %v", args, err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("kubectl %v exited %d: %s%s", args, result.ExitCode, result.Stdout, result.Stderr)
	}
}

func k3sWaitForEvent(t *testing.T, messages <-chan map[string]any, limit time.Duration, done func(map[string]any) bool) {
	t.Helper()
	deadline := time.After(limit)
	for {
		select {
		case message, ok := <-messages:
			if !ok {
				t.Fatal("the events socket closed before the expected message")
			}
			if done(message) {
				return
			}
		case <-deadline:
			t.Fatalf("timed out after %s waiting for an event", limit)
		}
	}
}

func k3sWaitForRunning(t *testing.T, messages <-chan map[string]any, start time.Time) {
	t.Helper()
	var steps []string
	k3sWaitForEvent(t, messages, k3sRunningLimit, func(message map[string]any) bool {
		switch message["type"] {
		case "provisioning":
			steps = append(steps, message["step"].(string))
		case "status":
			switch message["status"] {
			case store.StatusRunning:
				return true
			case store.StatusProvisioning:
			default:
				t.Fatalf("attempt reached %v while provisioning: %v", message["status"], message["error_message"])
			}
		}
		return false
	})
	if !slices.Contains(steps, "k3s") {
		t.Errorf("provisioning steps = %v, want a k3s step", steps)
	}
	t.Logf("running after %s, provisioning steps %v", time.Since(start).Round(time.Millisecond), steps)
}

func k3sWaitForPassed(t *testing.T, messages <-chan map[string]any, start time.Time) {
	t.Helper()
	k3sWaitForEvent(t, messages, k3sPassedLimit, func(message map[string]any) bool {
		if message["type"] == "checkpoint" {
			t.Logf("checkpoint %v = %v at %s", message["id"], message["status"], time.Since(start).Round(time.Millisecond))
		}
		if message["type"] != "status" {
			return false
		}
		switch message["status"] {
		case store.StatusPassed:
			return true
		case store.StatusRunning:
			return false
		default:
			t.Fatalf("attempt reached %v, want passed: %v", message["status"], message["error_message"])
			return false
		}
	})
	t.Logf("passed after %s", time.Since(start).Round(time.Millisecond))
}

func (s *k3sStack) result(t *testing.T, id string) map[string]any {
	t.Helper()
	result := s.request(t, http.MethodGet, "/api/attempts/"+id+"/result", "")
	if result["_status"] != float64(http.StatusOK) {
		t.Fatalf("result: %v", result)
	}
	return result
}

func TestK3sPodCrashloopLabPasses(t *testing.T) {
	s := newK3sStack(t)
	start := time.Now()

	id := s.start(t, "k3s-pod-01-crashloop")
	messages := s.events(t, id)
	k3sWaitForRunning(t, messages, start)

	params := s.params(t, id)
	s.kubectl(t, id, "-n", params["namespace"], "patch", "deploy", params["app"], "--type=json",
		`-p=[{"op":"replace","path":"/spec/template/spec/containers/0/command","value":["sleep","3600"]}]`)
	t.Logf("fix applied at %s", time.Since(start).Round(time.Millisecond))

	k3sWaitForPassed(t, messages, start)

	result := s.result(t, id)
	if result["status"] != store.StatusPassed {
		t.Errorf("result status = %v, want passed", result["status"])
	}
	if got := len(result["checkpoints"].([]any)); got != 2 {
		t.Errorf("result has %d checkpoints, want 2", got)
	}
}

func TestK3sServiceSelectorLabPasses(t *testing.T) {
	s := newK3sStack(t)
	start := time.Now()

	id := s.start(t, "k3s-svc-01-selector")
	messages := s.events(t, id)
	k3sWaitForRunning(t, messages, start)

	params := s.params(t, id)
	s.kubectl(t, id, "-n", params["namespace"], "patch", "svc", params["app"], "--type=merge",
		`-p={"spec":{"selector":{"app":"`+params["app"]+`"}}}`)
	s.kubectl(t, id, "-n", params["namespace"], "patch", "svc", params["app"], "--type=json",
		`-p=[{"op":"replace","path":"/spec/ports/0/targetPort","value":`+params["port"]+`}]`)
	t.Logf("fix applied at %s", time.Since(start).Round(time.Millisecond))

	k3sWaitForPassed(t, messages, start)

	result := s.result(t, id)
	hidden := map[string]bool{}
	for _, entry := range result["checkpoints"].([]any) {
		cp := entry.(map[string]any)
		if !cp["visible"].(bool) {
			hidden[cp["id"].(string)] = cp["status"] == store.CheckpointPass
		}
	}
	if passed, ok := hidden["pod-on-agent"]; !ok || !passed {
		t.Errorf("hidden checkpoints in the result = %v, want pod-on-agent passed and marked invisible", hidden)
	}
}
