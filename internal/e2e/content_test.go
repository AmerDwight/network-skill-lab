//go:build content_integration

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

const (
	contentImage     = "nsl/node"
	routeLab         = "net-route-01-wrong-gateway"
	dnsLab           = "net-dns-01-resolved"
	forwardingOff    = "gw-forwarding-off"
	contentDraws     = 8
	contentInFlight  = 6
	contentProvision = 3 * time.Minute
	contentCheck     = 90 * time.Second
)

const routeFix = `set -euo pipefail
ip route replace "$NSL_SUBNET_B" via "$NSL_IP_GW_A"
`

const forwardingFix = `set -euo pipefail
sysctl -w net.ipv4.ip_forward=1
`

const dnsFix = `set -euo pipefail
cat >/etc/netplan/60-nsl-dns.yaml <<EOF
network:
  version: 2
  ethernets:
    eth1:
      nameservers:
        addresses: [$NSL_IP_DNS]
        search: [$NSL_DOMAIN]
EOF
chmod 0600 /etc/netplan/60-nsl-dns.yaml
netplan apply
ln -sf /run/systemd/resolve/stub-resolv.conf /etc/resolv.conf
resolvectl flush-caches
`

type contentStack struct {
	server   *httptest.Server
	attempts *attempt.Service
	provider *docker.Provider
	store    *store.Store
}

func contentDockerClient(t *testing.T) client.APIClient {
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
	return cli
}

func newContentStack(t *testing.T, cli client.APIClient) *contentStack {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	loaded, err := content.LoadAll(filepath.Join("..", "..", "content"))
	if err != nil {
		t.Fatalf("load shipped content: %v", err)
	}
	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	provider := docker.New(cli, docker.Options{Image: contentImage})
	attempts := attempt.New(attempt.Deps{
		Store:    st,
		Runner:   provider,
		Content:  loaded,
		Image:    contentImage,
		RunnerID: "docker",
		Logger:   logger,
	})
	t.Cleanup(attempts.Close)

	checks := checker.New(checker.Deps{Store: st, Runner: provider, Attempts: attempts, Interval: 2 * time.Second, Logger: logger})
	t.Cleanup(checks.Close)
	attempts.SetSweeper(checks)
	recordings := recorder.New(recorder.Deps{Store: st, Runner: provider, Attempts: attempts, Interval: 2 * time.Second, DataDir: dataDir, Logger: logger})
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

	return &contentStack{server: server, attempts: attempts, provider: provider, store: st}
}

func (s *contentStack) request(t *testing.T, method, path, body string) map[string]any {
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

func (s *contentStack) start(t *testing.T, labID, mode string) string {
	t.Helper()
	created := s.request(t, http.MethodPost, "/api/attempts", fmt.Sprintf(`{"lab_id":%q,"mode":%q}`, labID, mode))
	if created["_status"] != float64(http.StatusCreated) {
		t.Fatalf("create %s attempt for %s: %v", mode, labID, created)
	}
	return created["id"].(string)
}

func (s *contentStack) cleanup(t *testing.T, id string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.WithoutCancel(t.Context())
		if _, err := s.attempts.AbandonAsAdmin(ctx, id); err != nil && !errors.Is(err, attempt.ErrTerminal) {
			t.Errorf("cleanup abandon: %v", err)
		}
		if err := s.provider.Destroy(ctx, runner.SandboxID(id)); err != nil {
			t.Logf("cleanup destroy: %v", err)
		}
	})
}

func (s *contentStack) caseOf(t *testing.T, id string) string {
	t.Helper()
	att, err := s.store.Attempts.Get(context.WithoutCancel(t.Context()), id)
	if err != nil {
		t.Fatalf("get attempt %s: %v", id, err)
	}
	return att.CaseID
}

func (s *contentStack) env(t *testing.T, id string) map[string]string {
	t.Helper()
	att, err := s.store.Attempts.Get(context.WithoutCancel(t.Context()), id)
	if err != nil {
		t.Fatalf("get attempt %s: %v", id, err)
	}
	var params map[string]string
	if err := json.Unmarshal([]byte(att.ParamsJSON), &params); err != nil {
		t.Fatalf("decode params of %s: %v", id, err)
	}
	return content.ParamsEnv(params)
}

func (s *contentStack) exec(t *testing.T, id, node, script string) {
	t.Helper()
	opts := runner.ExecOptions{Env: s.env(t, id), Stdin: bytes.NewReader([]byte(script)), Timeout: 60 * time.Second}
	result, err := s.provider.Exec(t.Context(), runner.SandboxID(id), node, []string{"bash", "-s"}, opts)
	if err != nil {
		t.Fatalf("exec on %s: %v", node, err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exec on %s exited with %d: %s", node, result.ExitCode, result.Stderr)
	}
}

func (s *contentStack) status(t *testing.T, id string) string {
	t.Helper()
	view := s.request(t, http.MethodGet, "/api/attempts/"+id, "")
	if view["_status"] != float64(http.StatusOK) {
		t.Fatalf("read attempt %s: %v", id, view)
	}
	return view["status"].(string)
}

func contentWaitFor(t *testing.T, what string, limit time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", limit, what)
}

func (s *contentStack) waitForRunning(t *testing.T, id string) {
	t.Helper()
	contentWaitFor(t, "attempt "+id+" to start running", contentProvision, func() bool {
		view := s.request(t, http.MethodGet, "/api/attempts/"+id, "")
		switch status := view["status"]; status {
		case store.StatusRunning:
			return true
		case store.StatusProvisioning:
			return false
		default:
			t.Fatalf("attempt %s reached %v while provisioning: %v", id, status, view["error_message"])
			return false
		}
	})
}

func (s *contentStack) passedWithin(t *testing.T, id string, limit time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		switch status := s.status(t, id); status {
		case store.StatusPassed:
			return true
		case store.StatusRunning:
			time.Sleep(500 * time.Millisecond)
		default:
			t.Fatalf("attempt %s reached %s instead of passing", id, status)
		}
	}
	return false
}

func (s *contentStack) waitForPassed(t *testing.T, id string) {
	t.Helper()
	if !s.passedWithin(t, id, contentCheck) {
		t.Fatalf("attempt %s did not pass within %s, checkpoints = %v", id, contentCheck, s.checkpointStatuses(t, id))
	}
}

func (s *contentStack) checkpointStatuses(t *testing.T, id string) map[string]string {
	t.Helper()
	runs, err := s.store.CheckpointRuns.ListByAttempt(context.WithoutCancel(t.Context()), id)
	if err != nil {
		t.Fatalf("list checkpoint runs: %v", err)
	}
	statuses := make(map[string]string, len(runs))
	for _, run := range runs {
		statuses[run.CheckpointID] = run.LastStatus
	}
	return statuses
}

func (s *contentStack) assertEveryCheckpointPassed(t *testing.T, id string, want int) {
	t.Helper()
	runs, err := s.store.CheckpointRuns.ListByAttempt(context.WithoutCancel(t.Context()), id)
	if err != nil {
		t.Fatalf("list checkpoint runs: %v", err)
	}
	if len(runs) != want {
		t.Fatalf("got %d checkpoint runs, want %d", len(runs), want)
	}
	for _, run := range runs {
		if run.LastStatus != store.CheckpointPass || run.FirstPassedAt == nil {
			t.Errorf("checkpoint run %s = %+v", run.CheckpointID, run)
		}
	}
}

func contentContainers(t *testing.T, cli client.APIClient, label string) []container.Summary {
	t.Helper()
	f := filters.NewArgs(filters.Arg("label", label))
	containers, err := cli.ContainerList(context.WithoutCancel(t.Context()), container.ListOptions{All: true, Filters: f})
	if err != nil {
		t.Fatalf("list containers: %v", err)
	}
	return containers
}

func assertNoLeftovers(t *testing.T, cli client.APIClient, id string) {
	t.Helper()
	contentWaitFor(t, "the sandbox of "+id+" to be gone", 2*time.Minute, func() bool {
		return len(contentContainers(t, cli, "nsl.attempt="+id)) == 0
	})
}

func throttleDraws(t *testing.T, cli client.APIClient) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if len(contentContainers(t, cli, "nsl.managed=true")) <= contentInFlight {
			return
		}
		time.Sleep(time.Second)
	}
	t.Log("gave up waiting for earlier sandboxes to be torn down, drawing anyway")
}

func TestContentRouteLabPassesInGuidedMode(t *testing.T) {
	cli := contentDockerClient(t)
	s := newContentStack(t, cli)
	start := time.Now()

	id := s.start(t, routeLab, "guided")
	s.cleanup(t, id)
	t.Logf("attempt %s drew case %q", id, s.caseOf(t, id))

	s.waitForRunning(t, id)
	t.Logf("running after %s", time.Since(start).Round(time.Millisecond))

	s.exec(t, id, "web01", routeFix)
	t.Logf("route repaired at %s", time.Since(start).Round(time.Millisecond))

	if !s.passedWithin(t, id, 20*time.Second) {
		if drawn := s.caseOf(t, id); drawn != forwardingOff {
			t.Fatalf("case %q did not pass on the route fix alone, checkpoints = %v", drawn, s.checkpointStatuses(t, id))
		}
		s.exec(t, id, "gw01", forwardingFix)
		t.Logf("forwarding repaired at %s", time.Since(start).Round(time.Millisecond))
		s.waitForPassed(t, id)
	}
	s.assertEveryCheckpointPassed(t, id, 3)
	t.Logf("case %q passed after %s", s.caseOf(t, id), time.Since(start).Round(time.Millisecond))

	assertNoLeftovers(t, cli, id)
	t.Logf("sandbox destroyed at %s", time.Since(start).Round(time.Millisecond))
}

func TestContentDNSLabPassesInGuidedMode(t *testing.T) {
	cli := contentDockerClient(t)
	s := newContentStack(t, cli)
	start := time.Now()

	id := s.start(t, dnsLab, "guided")
	s.cleanup(t, id)
	drawn := s.caseOf(t, id)
	t.Logf("attempt %s drew case %q", id, drawn)

	s.waitForRunning(t, id)
	t.Logf("running after %s", time.Since(start).Round(time.Millisecond))

	s.exec(t, id, "web01", dnsFix)
	t.Logf("resolver repaired at %s", time.Since(start).Round(time.Millisecond))

	s.waitForPassed(t, id)
	s.assertEveryCheckpointPassed(t, id, 3)
	t.Logf("case %q passed after %s", drawn, time.Since(start).Round(time.Millisecond))

	assertNoLeftovers(t, cli, id)
	t.Logf("sandbox destroyed at %s", time.Since(start).Round(time.Millisecond))
}

func TestContentRouteLabHidesTheForwardingCheckpointInRealMode(t *testing.T) {
	cli := contentDockerClient(t)
	s := newContentStack(t, cli)
	start := time.Now()

	id := ""
	drawn := ""
	for draw := 1; draw <= contentDraws && drawn != forwardingOff; draw++ {
		throttleDraws(t, cli)
		id = s.start(t, routeLab, "real")
		drawn = s.caseOf(t, id)
		t.Logf("draw %d: attempt %s drew case %q", draw, id, drawn)
		if drawn != forwardingOff {
			if _, err := s.attempts.AbandonAsAdmin(context.WithoutCancel(t.Context()), id); err != nil {
				t.Fatalf("abandon draw %d: %v", draw, err)
			}
		}
	}
	if drawn != forwardingOff {
		assertNoLeftovers(t, cli, id)
		t.Skipf("case %q was never drawn in %d attempts, which happens about 10%% of the time at weights 3:1", forwardingOff, contentDraws)
	}
	s.cleanup(t, id)
	t.Logf("case %q drawn after %s", forwardingOff, time.Since(start).Round(time.Millisecond))

	s.waitForRunning(t, id)
	t.Logf("running after %s", time.Since(start).Round(time.Millisecond))

	first := s.request(t, http.MethodPost, "/api/attempts/"+id+"/submit", "")
	if first["_status"] != float64(http.StatusOK) {
		t.Fatalf("first submit: %v", first)
	}
	if first["passed"] != false {
		t.Errorf("first submit passed = %v, want false", first["passed"])
	}
	if first["hidden_failed"] != float64(1) {
		t.Errorf("first submit hidden_failed = %v, want 1", first["hidden_failed"])
	}
	revealed := first["checkpoints"].([]any)
	if len(revealed) != 2 {
		t.Errorf("first submit revealed %d checkpoints, want only the visible 2", len(revealed))
	}
	for _, entry := range revealed {
		if id := entry.(map[string]any)["id"]; id == "forwarding-on" {
			t.Errorf("the hidden checkpoint was revealed by a submit: %v", entry)
		}
	}
	t.Logf("first submit at %s: %v", time.Since(start).Round(time.Millisecond), first)

	s.exec(t, id, "web01", routeFix)
	s.exec(t, id, "gw01", forwardingFix)
	t.Logf("both faults repaired at %s", time.Since(start).Round(time.Millisecond))

	second := map[string]any{}
	contentWaitFor(t, "a submit that passes", contentCheck, func() bool {
		second = s.request(t, http.MethodPost, "/api/attempts/"+id+"/submit", "")
		return second["_status"] == float64(http.StatusOK) && second["passed"] == true
	})
	if second["hidden_failed"] != float64(0) {
		t.Errorf("passing submit hidden_failed = %v, want 0", second["hidden_failed"])
	}
	t.Logf("passing submit at %s: %v", time.Since(start).Round(time.Millisecond), second)

	result := s.request(t, http.MethodGet, "/api/attempts/"+id+"/result", "")
	if result["status"] != store.StatusPassed {
		t.Fatalf("result: %v", result)
	}
	if entries := result["checkpoints"].([]any); len(entries) != 3 {
		t.Errorf("the result page lists %d checkpoints, want all 3 including the hidden one", len(entries))
	}

	assertNoLeftovers(t, cli, id)
	t.Logf("sandbox destroyed at %s", time.Since(start).Round(time.Millisecond))
}
