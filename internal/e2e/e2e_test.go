//go:build integration

package e2e

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

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
	testImage  = "nsl/node"
	contentDir = "../../content"
	marker     = "echo nsl-e2e-marker"
)

type terminal struct {
	pty runner.PTY

	mu     sync.Mutex
	output strings.Builder
}

func (term *terminal) consume() {
	buf := make([]byte, 4096)
	for {
		n, err := term.pty.Read(buf)
		if n > 0 {
			term.mu.Lock()
			term.output.Write(buf[:n])
			term.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (term *terminal) seen() string {
	term.mu.Lock()
	defer term.mu.Unlock()
	return term.output.String()
}

func dockerClient(t *testing.T) client.APIClient {
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

func waitFor(t *testing.T, what string, limit time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", limit, what)
}

func TestFixtureLabRunsToPassed(t *testing.T) {
	cli := dockerClient(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	labs, err := content.Load(contentDir)
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	user, err := st.Users.Local(t.Context())
	if err != nil {
		t.Fatalf("local user: %v", err)
	}

	provider := docker.New(cli, docker.Options{Image: testImage})
	svc := attempt.New(attempt.Deps{
		Store:    st,
		Runner:   provider,
		Labs:     labs,
		Image:    testImage,
		RunnerID: "docker",
		Logger:   logger,
	})
	chk := checker.New(checker.Deps{Store: st, Runner: provider, Attempts: svc, Interval: time.Second, Logger: logger})
	rec := recorder.New(recorder.Deps{Store: st, Runner: provider, Attempts: svc, Interval: time.Second, DataDir: dataDir, Logger: logger})

	events, unsubscribe := svc.SubscribeAll()
	t.Cleanup(unsubscribe)

	chk.Start(t.Context())
	rec.Start(t.Context())

	start := time.Now()
	view, err := svc.Start(t.Context(), user.ID, labs[0].Id, "guided")
	if err != nil {
		t.Fatalf("start attempt: %v", err)
	}
	id := view.Id
	t.Cleanup(func() {
		chk.Close()
		rec.Close()
		ctx := context.WithoutCancel(t.Context())
		if _, err := svc.Abandon(ctx, id); err != nil && !errors.Is(err, attempt.ErrTerminal) {
			t.Errorf("cleanup abandon: %v", err)
		}
		svc.Close()
		if err := provider.Destroy(ctx, runner.SandboxID(id)); err != nil {
			t.Errorf("cleanup destroy: %v", err)
		}
	})

	statuses := make(chan string, 16)
	checkpoints := make(chan attempt.CheckpointEvent, 64)
	go func() {
		for ev := range events {
			switch ev.Type {
			case attempt.EventStatus:
				statuses <- ev.Status
			case attempt.EventCheckpoint:
				checkpoints <- *ev.Checkpoint
			}
		}
	}()

	waitForStatus(t, statuses, store.StatusRunning, 3*time.Minute)
	t.Logf("provisioning took %s", time.Since(start).Round(time.Millisecond))

	seen := map[string]string{}
	for range 2 {
		select {
		case ev := <-checkpoints:
			if _, ok := seen[ev.Id]; ok {
				t.Fatalf("checkpoint %s reported twice before any fix", ev.Id)
			}
			seen[ev.Id] = ev.Status
		case <-time.After(30 * time.Second):
			t.Fatalf("only saw checkpoint events %v within 30s", seen)
		}
	}
	for cpID, status := range seen {
		if status != store.CheckpointFail {
			t.Errorf("first status of checkpoint %s = %s, want fail", cpID, status)
		}
	}
	t.Logf("first checkpoint sweep at %s: %v", time.Since(start).Round(time.Millisecond), seen)

	pty, err := provider.OpenTerminal(t.Context(), runner.SandboxID(id), "web01")
	if err != nil {
		t.Fatalf("open terminal: %v", err)
	}
	term := &terminal{pty: pty}
	go term.consume()
	t.Cleanup(func() { _ = pty.Close() })

	if _, err := pty.Write([]byte("sudo ip link set eth1 up\n")); err != nil {
		t.Fatalf("write to terminal: %v", err)
	}
	waitFor(t, "the shell to echo the fix", 10*time.Second, func() bool {
		return strings.Contains(term.seen(), "ip link set eth1 up")
	})
	if _, err := pty.Write([]byte(marker + "\n")); err != nil {
		t.Fatalf("write marker to terminal: %v", err)
	}
	waitFor(t, "the marker output", 10*time.Second, func() bool {
		return strings.Count(term.seen(), "nsl-e2e-marker") >= 2
	})

	passed := map[string]bool{}
	deadline := time.Now().Add(30 * time.Second)
	for len(passed) < 2 {
		select {
		case ev := <-checkpoints:
			if ev.Status == store.CheckpointPass {
				passed[ev.Id] = true
			} else {
				delete(passed, ev.Id)
			}
		case <-time.After(time.Until(deadline)):
			t.Fatalf("checkpoints did not all pass within 30s, passed = %v", passed)
		}
	}
	t.Logf("all checkpoints passed at %s", time.Since(start).Round(time.Millisecond))

	waitForStatus(t, statuses, store.StatusPassed, 30*time.Second)
	t.Logf("attempt passed at %s", time.Since(start).Round(time.Millisecond))

	waitFor(t, "the command log to contain the marker", 10*time.Second, func() bool {
		entries, err := st.CommandLog.ListByAttempt(t.Context(), id)
		if err != nil {
			t.Fatalf("list command log: %v", err)
		}
		for _, entry := range entries {
			if strings.Contains(entry.Command, marker) {
				return true
			}
		}
		return false
	})
	t.Logf("command log complete at %s", time.Since(start).Round(time.Millisecond))

	count, err := st.CommandLog.CountByAttempt(t.Context(), id)
	if err != nil {
		t.Fatalf("count command log: %v", err)
	}
	if count < 2 {
		t.Errorf("command log has %d entries, want at least 2", count)
	}

	runs, err := st.CheckpointRuns.ListByAttempt(t.Context(), id)
	if err != nil {
		t.Fatalf("list checkpoint runs: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("got %d checkpoint runs, want 2", len(runs))
	}
	for _, run := range runs {
		if run.LastStatus != store.CheckpointPass || run.FirstPassedAt == nil {
			t.Errorf("checkpoint run %s = %+v", run.CheckpointID, run)
		}
	}

	waitFor(t, "the sandbox to be destroyed", 60*time.Second, func() bool {
		return len(containersOf(t, cli, id)) == 0
	})
	t.Logf("sandbox destroyed at %s", time.Since(start).Round(time.Millisecond))
}

func waitForStatus(t *testing.T, statuses <-chan string, want string, limit time.Duration) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for {
		select {
		case status := <-statuses:
			if status == want {
				return
			}
			if status != store.StatusProvisioning && status != store.StatusRunning {
				t.Fatalf("attempt reached %s, want %s", status, want)
			}
		case <-time.After(time.Until(deadline)):
			t.Fatalf("timed out after %s waiting for status %s", limit, want)
		}
	}
}

func containersOf(t *testing.T, cli client.APIClient, attemptID string) []container.Summary {
	t.Helper()
	f := filters.NewArgs(filters.Arg("label", "nsl.attempt="+attemptID))
	containers, err := cli.ContainerList(context.WithoutCancel(t.Context()), container.ListOptions{All: true, Filters: f})
	if err != nil {
		t.Fatalf("list containers: %v", err)
	}
	return containers
}
