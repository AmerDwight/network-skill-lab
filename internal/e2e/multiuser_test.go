//go:build integration

package e2e

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/attempt"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/store"
	"github.com/coder/websocket"
)

const fixtureLab = "net-ip-01-link-down"

func (s *stack) cleanupAttempt(t *testing.T, id string) {
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

func (s *stack) waitForStatus(t *testing.T, client *http.Client, id, want string) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Minute)
	for time.Now().Before(deadline) {
		got := s.requestAs(t, client, http.MethodGet, "/api/attempts/"+id, "")
		switch got["status"] {
		case want:
			return
		case store.StatusError, store.StatusAbandoned, store.StatusExpired:
			t.Fatalf("attempt %s reached %v: %v", id, got["status"], got["error_message"])
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("attempt %s did not reach %s in time", id, want)
}

func (s *stack) solve(t *testing.T, client *http.Client, id string) {
	t.Helper()
	conn := s.dialAs(t, client, "/ws/attempts/"+id+"/term/web01/main")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageBinary, []byte(linkUpCommand)); err != nil {
		t.Fatalf("send the fix to %s: %v", id, err)
	}
}

func TestTwoUsersRunTheirOwnAttempts(t *testing.T) {
	cli := dockerClient(t)
	s := newStackWith(t, cli, 2)
	bob := s.newUser(t, "bob")
	clients := map[string]*http.Client{"alice": s.client, "bob": bob}

	var (
		mu  sync.Mutex
		ids = map[string]string{}
		wg  sync.WaitGroup
	)
	for name, client := range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			created := s.requestAs(t, client, http.MethodPost, "/api/attempts",
				fmt.Sprintf(`{"lab_id":%q,"mode":"guided"}`, fixtureLab))
			if created["_status"] != float64(http.StatusCreated) {
				t.Errorf("create attempt for %s: %v", name, created)
				return
			}
			mu.Lock()
			ids[name] = created["id"].(string)
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(ids) != 2 {
		t.Fatalf("started %d attempts, want 2", len(ids))
	}
	for _, id := range ids {
		s.cleanupAttempt(t, id)
	}

	for name, id := range ids {
		s.waitForStatus(t, clients[name], id, store.StatusRunning)
		t.Logf("%s is running %s", name, id)
	}

	if got := s.requestAs(t, clients["bob"], http.MethodGet, "/api/attempts/"+ids["alice"], ""); got["_status"] != float64(http.StatusNotFound) {
		t.Errorf("bob reading alice's attempt = %v, want 404", got)
	}
	if got := s.requestAs(t, clients["alice"], http.MethodGet, "/api/attempts/"+ids["bob"], ""); got["_status"] != float64(http.StatusNotFound) {
		t.Errorf("alice reading bob's attempt = %v, want 404", got)
	}

	for name, id := range ids {
		s.solve(t, clients[name], id)
	}
	for name, id := range ids {
		s.waitForStatus(t, clients[name], id, store.StatusPassed)
		t.Logf("%s passed %s", name, id)
	}

	for name, id := range ids {
		history := s.requestArrayAs(t, clients[name], http.MethodGet, "/api/history")
		if len(history) != 1 || history[0].(map[string]any)["id"] != id {
			t.Errorf("history of %s = %v, want only %s", name, history, id)
		}
	}
}

func TestRunnerBusyWhenTheSandboxLimitIsReached(t *testing.T) {
	cli := dockerClient(t)
	s := newStackWith(t, cli, 1)
	bob := s.newUser(t, "bob")

	created := s.request(t, http.MethodPost, "/api/attempts",
		fmt.Sprintf(`{"lab_id":%q,"mode":"guided"}`, fixtureLab))
	if created["_status"] != float64(http.StatusCreated) {
		t.Fatalf("create attempt: %v", created)
	}
	s.cleanupAttempt(t, created["id"].(string))

	refused := s.requestAs(t, bob, http.MethodPost, "/api/attempts",
		fmt.Sprintf(`{"lab_id":%q,"mode":"guided"}`, fixtureLab))
	if refused["_status"] != float64(http.StatusTooManyRequests) {
		t.Fatalf("second start = %v, want 429", refused)
	}
	if refused["sandboxes_active"] != float64(1) || refused["sandboxes_max"] != float64(1) {
		t.Errorf("busy body = %v", refused)
	}
	failure := refused["error"].(map[string]any)
	if failure["code"] != "runner_busy" {
		t.Errorf("error.code = %v, want runner_busy", failure["code"])
	}
}
