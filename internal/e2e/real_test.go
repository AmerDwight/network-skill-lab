//go:build integration

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/attempt"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/store"
	"github.com/coder/websocket"
)

func TestRealModeHidesProgressUntilSubmit(t *testing.T) {
	cli := dockerClient(t)
	s := newStack(t, cli)
	start := time.Now()

	created := s.request(t, http.MethodPost, "/api/attempts", `{"lab_id":"net-ip-01-link-down","mode":"real"}`)
	if created["_status"] != float64(http.StatusCreated) {
		t.Fatalf("create attempt: %v", created)
	}
	id := created["id"].(string)
	t.Cleanup(func() {
		ctx := context.WithoutCancel(t.Context())
		if _, err := s.attempts.AbandonAsAdmin(ctx, id); err != nil && !errors.Is(err, attempt.ErrTerminal) {
			t.Errorf("cleanup abandon: %v", err)
		}
		if err := s.provider.Destroy(ctx, runner.SandboxID(id)); err != nil {
			t.Logf("cleanup destroy: %v", err)
		}
	})
	if created["checkpoints_hidden"] != true {
		t.Errorf("checkpoints_hidden = %v, want true", created["checkpoints_hidden"])
	}
	if entries := created["checkpoints"].([]any); len(entries) != 0 {
		t.Errorf("checkpoints = %v, want an empty list", entries)
	}

	events := s.dial(t, "/ws/attempts/"+id+"/events")
	messages := make(chan map[string]any, 256)
	go func() {
		defer close(messages)
		for {
			kind, data, err := events.Read(context.WithoutCancel(t.Context()))
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

	waitForEvent(t, messages, 3*time.Minute, func(message map[string]any) bool {
		if message["type"] != "status" {
			return false
		}
		if message["status"] == store.StatusRunning {
			return true
		}
		if message["status"] != store.StatusProvisioning {
			t.Fatalf("attempt reached %v while provisioning", message["status"])
		}
		return false
	})
	t.Logf("running after %s", time.Since(start).Round(time.Millisecond))

	term := s.dial(t, "/ws/attempts/"+id+"/term/web01/main")
	writeCtx, cancelWrite := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancelWrite()
	if err := term.Write(writeCtx, websocket.MessageBinary, []byte(probeCommand)); err != nil {
		t.Fatalf("send the probe command: %v", err)
	}
	if err := term.Write(writeCtx, websocket.MessageBinary, []byte(linkUpCommand)); err != nil {
		t.Fatalf("send the fix: %v", err)
	}

	waitFor(t, "the background sweep to record every checkpoint as passed", 90*time.Second, func() bool {
		runs, err := s.store.CheckpointRuns.ListByAttempt(context.WithoutCancel(t.Context()), id)
		if err != nil || len(runs) != 2 {
			return false
		}
		for _, run := range runs {
			if run.LastStatus != store.CheckpointPass {
				return false
			}
		}
		return true
	})
	t.Logf("checkpoints recorded after %s", time.Since(start).Round(time.Millisecond))

	for drained := false; !drained; {
		select {
		case message, ok := <-messages:
			if !ok {
				t.Fatal("the events socket closed before the submit")
			}
			if message["type"] == "checkpoint" {
				t.Fatalf("a checkpoint event reached a real-mode attempt: %v", message)
			}
			if message["type"] == "status" && message["status"] != store.StatusRunning {
				t.Fatalf("the attempt reached %v without a submit", message["status"])
			}
		default:
			drained = true
		}
	}

	live := s.request(t, http.MethodGet, "/api/attempts/"+id, "")
	if live["status"] != store.StatusRunning {
		t.Fatalf("status = %v, want the background sweep to leave the attempt running", live["status"])
	}

	submitted := s.request(t, http.MethodPost, "/api/attempts/"+id+"/submit", "")
	if submitted["_status"] != float64(http.StatusOK) || submitted["passed"] != true {
		t.Fatalf("submit: %v", submitted)
	}
	if submitted["hidden_failed"] != float64(0) || submitted["submit_count"] != float64(1) {
		t.Errorf("submit result = %v", submitted)
	}

	if err := term.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Errorf("close the terminal socket: %v", err)
	}

	result := s.request(t, http.MethodGet, "/api/attempts/"+id+"/result", "")
	if result["status"] != store.StatusPassed {
		t.Fatalf("result: %v", result)
	}
	if result["submit_count"] != float64(1) {
		t.Errorf("submit_count = %v, want 1", result["submit_count"])
	}
	if count := result["command_count"].(float64); count < 1 {
		t.Errorf("command_count = %v, want at least 1", count)
	}

	s.requestArray(t, http.MethodGet, "/api/tracks")
	s.requestArray(t, http.MethodGet, "/api/topics")

	waitFor(t, "every managed container to be gone", 60*time.Second, func() bool {
		return len(managedContainers(t, cli)) == 0
	})
	t.Logf("sandbox destroyed at %s", time.Since(start).Round(time.Millisecond))
}
