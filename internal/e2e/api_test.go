//go:build integration

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

const (
	probeCommand  = "ip -br link show eth1\r"
	linkUpCommand = "sudo ip link set eth1 up\r"
)

type stack struct {
	server   *httptest.Server
	attempts *attempt.Service
	provider *docker.Provider
	store    *store.Store
	dataDir  string
}

func newStack(t *testing.T, cli client.APIClient) *stack {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	loaded, err := content.LoadAll(contentDir)
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	provider := docker.New(cli, docker.Options{Image: testImage})
	attempts := attempt.New(attempt.Deps{
		Store:    st,
		Runner:   provider,
		Content:  loaded,
		Image:    testImage,
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

	return &stack{server: server, attempts: attempts, provider: provider, store: st, dataDir: dataDir}
}

func (s *stack) request(t *testing.T, method, path, body string) map[string]any {
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

func (s *stack) requestArray(t *testing.T, method, path string) []any {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, s.server.URL+path, nil)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, path, err)
	}
	resp, err := s.server.Client().Do(req)
	if err != nil {
		t.Fatalf("send request %s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s %s: status = %d", method, path, resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body of %s %s: %v", method, path, err)
	}
	var decoded []any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode body %q: %v", raw, err)
	}
	return decoded
}

func (s *stack) dial(t *testing.T, path string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(s.server.URL, "http")+path, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

func TestAPIDrivesFixtureLabToPassed(t *testing.T) {
	cli := dockerClient(t)
	s := newStack(t, cli)
	start := time.Now()

	created := s.request(t, http.MethodPost, "/api/attempts", `{"lab_id":"net-ip-01-link-down","mode":"guided"}`)
	if created["_status"] != float64(http.StatusCreated) {
		t.Fatalf("create attempt: %v", created)
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
	t.Logf("attempt %s created at %s", id, time.Since(start).Round(time.Millisecond))

	events := s.dial(t, "/ws/attempts/"+id+"/events")
	messages := make(chan map[string]any, 256)
	closed := make(chan error, 1)
	go func() {
		defer close(messages)
		for {
			kind, data, err := events.Read(context.WithoutCancel(t.Context()))
			if err != nil {
				closed <- err
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

	var provisioningSteps int
	waitForEvent(t, messages, 3*time.Minute, func(message map[string]any) bool {
		switch message["type"] {
		case "provisioning":
			provisioningSteps++
		case "status":
			if message["status"] == store.StatusRunning {
				return true
			}
			if message["status"] != store.StatusProvisioning {
				t.Fatalf("attempt reached %v while provisioning", message["status"])
			}
		}
		return false
	})
	if provisioningSteps == 0 {
		t.Error("no provisioning message arrived before running")
	}
	t.Logf("running after %s with %d provisioning messages", time.Since(start).Round(time.Millisecond), provisioningSteps)

	term := s.dial(t, "/ws/attempts/"+id+"/term/web01/main")
	writeCtx, cancelWrite := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancelWrite()
	if err := term.Write(writeCtx, websocket.MessageText, []byte(`{"type":"resize","cols":100,"rows":30}`)); err != nil {
		t.Fatalf("send resize: %v", err)
	}
	if err := term.Write(writeCtx, websocket.MessageBinary, []byte(probeCommand)); err != nil {
		t.Fatalf("send the probe command: %v", err)
	}
	if err := term.Write(writeCtx, websocket.MessageBinary, []byte(linkUpCommand)); err != nil {
		t.Fatalf("send the fix: %v", err)
	}
	t.Logf("probe and fix sent at %s", time.Since(start).Round(time.Millisecond))

	passed := map[string]bool{}
	waitForEvent(t, messages, 60*time.Second, func(message map[string]any) bool {
		switch message["type"] {
		case "checkpoint":
			if message["status"] == store.CheckpointPass {
				passed[message["id"].(string)] = true
			} else {
				delete(passed, message["id"].(string))
			}
		case "status":
			if message["status"] == store.StatusPassed {
				return true
			}
		}
		return false
	})
	if len(passed) != 2 {
		t.Errorf("passed checkpoints = %v, want both", passed)
	}
	t.Logf("passed after %s", time.Since(start).Round(time.Millisecond))

	select {
	case err := <-closed:
		if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
			t.Errorf("the events socket ended with %v, want a normal closure", err)
		}
	case <-time.After(10 * time.Second):
		t.Error("the events socket stayed open after the terminal status")
	}

	if err := term.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Errorf("close the terminal socket: %v", err)
	}

	result := s.request(t, http.MethodGet, "/api/attempts/"+id+"/result", "")
	if result["_status"] != float64(http.StatusOK) {
		t.Fatalf("result: %v", result)
	}
	if count := result["command_count"].(float64); count < 2 {
		t.Errorf("command_count = %v, want at least 2", count)
	}
	if solution := result["solution"].(string); solution == "" {
		t.Error("solution is empty")
	}
	t.Logf("result read at %s: command_count = %v", time.Since(start).Round(time.Millisecond), result["command_count"])

	var header string
	path := filepath.Join(s.dataDir, "recordings", id, "web01-main.cast")
	waitFor(t, "the recording to be flushed", 30*time.Second, func() bool {
		cast, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		header, _, _ = strings.Cut(string(cast), "\n")
		return header != ""
	})
	var decoded struct {
		Version int `json:"version"`
		Width   int `json:"width"`
	}
	if err := json.Unmarshal([]byte(header), &decoded); err != nil {
		t.Fatalf("decode recording header %q: %v", header, err)
	}
	if decoded.Version != 2 || decoded.Width != 80 {
		t.Errorf("recording header = %q", header)
	}

	waitFor(t, "every managed container to be gone", 60*time.Second, func() bool {
		return len(managedContainers(t, cli)) == 0
	})
	t.Logf("sandbox destroyed at %s", time.Since(start).Round(time.Millisecond))
}

func waitForEvent(t *testing.T, messages <-chan map[string]any, limit time.Duration, done func(map[string]any) bool) {
	t.Helper()
	deadline := time.After(limit)
	for {
		select {
		case message, ok := <-messages:
			if !ok {
				t.Fatalf("the events socket closed before the expected message")
			}
			if done(message) {
				return
			}
		case <-deadline:
			t.Fatalf("timed out after %s waiting for an event", limit)
		}
	}
}

func managedContainers(t *testing.T, cli client.APIClient) []container.Summary {
	t.Helper()
	f := filters.NewArgs(filters.Arg("label", "nsl.managed=true"))
	containers, err := cli.ContainerList(context.WithoutCancel(t.Context()), container.ListOptions{All: true, Filters: f})
	if err != nil {
		t.Fatalf("list containers: %v", err)
	}
	return containers
}
