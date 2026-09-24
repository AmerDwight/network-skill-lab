//go:build integration

package e2e

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/attempt"
	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/provider/docker"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

const precheckLabYAML = `id: net-precheck-e2e
version: 1
title: { zh: "預檢重生", en: "Precheck respawn" }
topic: net/ip
level: 1
modes: [guided]
environment: container
estimated_minutes: 5
internet: false
ticket: { zh: "coin={{coin}}", en: "coin={{coin}}" }
params:
  coin: { gen: int, min: 1, max: 2 }
cases:
  - { file: cases/default.yaml, weight: 1 }
precheck: { script: precheck.sh, retries: 5 }
setup: setup.sh
checkpoints:
  - id: always-pass
    title: { zh: "通過", en: "passes" }
    node: web01
    script: checks/pass.sh
solution: { zh: solution.zh.md, en: solution.en.md }
`

const precheckTopologyYAML = `nodes:
  web01: { role: ubuntu }
`

func writePrecheckContent(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	dir := filepath.Join(root, "labs", "net-precheck-e2e")
	if err := os.MkdirAll(filepath.Join(dir, "checks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "cases"), 0o755); err != nil {
		t.Fatal(err)
	}

	files := map[string]struct {
		data string
		mode os.FileMode
	}{
		"lab.yaml":      {precheckLabYAML, 0o644},
		"topology.yaml": {precheckTopologyYAML, 0o644},
		"setup.sh":      {"#!/usr/bin/env bash\nset -e\n", 0o755},
		"precheck.sh": {`#!/usr/bin/env bash
set -e
if [ "$NSL_COIN" != "2" ]; then
  echo "coin is $NSL_COIN, want 2" >&2
  exit 1
fi
`, 0o755},
		"checks/pass.sh":     {"#!/usr/bin/env bash\nexit 0\n", 0o755},
		"cases/default.yaml": {"setup_env: { NSL_MARKER: \"e2e\" }\n", 0o644},
		"solution.zh.md":     {"zh\n", 0o644},
		"solution.en.md":     {"en\n", 0o644},
	}
	for name, file := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.WriteFile(path, []byte(file.data), file.mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, file.mode); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestPrecheckRespawnsUntilTheSandboxFitsTheCase(t *testing.T) {
	cli := dockerClient(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	labs, err := content.Load(writePrecheckContent(t))
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
		Content:  &content.Content{Labs: labs},
		Image:    testImage,
		RunnerID: "docker",
		Logger:   logger,
	})
	t.Cleanup(svc.Close)

	events, unsubscribe := svc.SubscribeAll()
	t.Cleanup(unsubscribe)

	retries := make(chan int, 16)
	statuses := make(chan string, 16)
	go func() {
		for ev := range events {
			switch {
			case ev.Type == attempt.EventProvisioning && ev.Step == attempt.StepPrecheck:
				retries <- ev.Attempt
			case ev.Type == attempt.EventStatus:
				statuses <- ev.Status
			}
		}
	}()

	deadline := time.Now().Add(45 * time.Second)
	sawRetry := false
	for round := 1; !sawRetry && time.Now().Before(deadline); round++ {
		view, err := svc.Start(t.Context(), user.ID, labs[0].Id, "guided")
		if err != nil {
			t.Fatalf("start attempt %d: %v", round, err)
		}
		id := view.Id
		if view.Seed == nil {
			t.Fatal("the attempt was started without a seed")
		}

		status := awaitStatus(t, statuses, 3*time.Minute, store.StatusRunning, store.StatusError)
		attempts := 1
		for drained := true; drained; {
			select {
			case n := <-retries:
				sawRetry = true
				attempts = n
				if n < 2 {
					t.Errorf("retry event carried attempt %d, want at least 2", n)
				}
			default:
				drained = false
			}
		}

		att, err := st.Attempts.Get(t.Context(), id)
		if err != nil {
			t.Fatalf("get attempt: %v", err)
		}
		if att.CaseID != "default" {
			t.Errorf("stored case id = %q, want %q", att.CaseID, "default")
		}
		if att.Seed == nil {
			t.Fatal("the seed was not stored")
		}
		if *att.Seed != *view.Seed+int64(attempts-1) {
			t.Errorf("seed = %d, want %d after %d provisioning attempts", *att.Seed, *view.Seed+int64(attempts-1), attempts)
		}
		t.Logf("round %d: attempt %s ended in %s after %d provisioning attempts", round, id, status, attempts)

		ctx := context.WithoutCancel(t.Context())
		if status == store.StatusRunning {
			if _, err := svc.AbandonAsAdmin(ctx, id); err != nil && !errors.Is(err, attempt.ErrTerminal) {
				t.Fatalf("abandon: %v", err)
			}
			awaitStatus(t, statuses, 60*time.Second, store.StatusAbandoned)
		}
		waitFor(t, "the sandbox to be destroyed", 60*time.Second, func() bool {
			return len(containersOf(t, cli, id)) == 0
		})
		if err := provider.Destroy(ctx, runner.SandboxID(id)); err != nil {
			t.Errorf("cleanup destroy: %v", err)
		}
	}

	if !sawRetry {
		t.Log("no attempt needed a precheck retry within the time budget")
	}
}

func awaitStatus(t *testing.T, statuses <-chan string, limit time.Duration, want ...string) string {
	t.Helper()
	deadline := time.After(limit)
	for {
		select {
		case status := <-statuses:
			if slices.Contains(want, status) {
				return status
			}
			if status != store.StatusProvisioning {
				t.Fatalf("attempt reached %s, want one of %v", status, want)
			}
		case <-deadline:
			t.Fatalf("no %v status within %s", want, limit)
		}
	}
}
