package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/AmerDwight/network-skill-lab/internal/content/contenttest"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/runnerclient"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

func (h *harness) createRunner(body string) map[string]any {
	h.t.Helper()
	resp := h.send(h.adminClient(), http.MethodPost, "/api/admin/runners", body)
	if resp.StatusCode != http.StatusCreated {
		h.t.Fatalf("create runner: status = %d", resp.StatusCode)
	}
	return decodeJSON(h.t, resp)
}

func (h *harness) listRunners() []map[string]any {
	h.t.Helper()
	resp := h.send(h.adminClient(), http.MethodGet, "/api/admin/runners", "")
	if resp.StatusCode != http.StatusOK {
		h.t.Fatalf("list runners: status = %d", resp.StatusCode)
	}
	var runners []map[string]any
	for _, raw := range decodeArray(h.t, resp) {
		runners = append(runners, raw.(map[string]any))
	}
	return runners
}

func (h *harness) runnerNamed(name string) map[string]any {
	h.t.Helper()
	for _, r := range h.listRunners() {
		if r["name"] == name {
			return r
		}
	}
	h.t.Fatalf("runner %s is not listed", name)
	return nil
}

func (h *harness) connectRunner(name, token string) (*fakeRunner, <-chan error) {
	h.t.Helper()
	provider := newFakeRunner()
	ctx, cancel := context.WithCancel(context.WithoutCancel(h.t.Context()))
	done := make(chan error, 1)
	go func() {
		done <- runnerclient.Run(ctx, runnerclient.Options{
			URL:      h.socketURL("/ws/runner"),
			Token:    token,
			Name:     name,
			Capacity: 2,
			Version:  "test",
			Provider: provider,
			Logger:   discardLogger(),
		})
	}()
	h.t.Cleanup(cancel)
	return provider, done
}

func TestAdminRunnersCreateAndList(t *testing.T) {
	h := newHarness(t)
	admin := h.adminClient()

	created := h.createRunner(`{"name":"lab-01"}`)
	requireKeys(t, created, "id", "name", "token")
	if created["name"] != "lab-01" || created["token"] == "" {
		t.Errorf("created = %v", created)
	}
	h.createRunner(`{"name":"kvm-01","capabilities":["container","vm"],"capacity":1}`)

	requireError(t, h.send(admin, http.MethodPost, "/api/admin/runners", `{"name":"lab-01"}`), http.StatusConflict, "runner_exists")
	for _, body := range []string{
		`{"name":"No Spaces"}`,
		`{"name":"local"}`,
		`{"name":"lab-02","capabilities":["gpu"]}`,
		`{"name":"lab-02","capacity":0}`,
	} {
		requireError(t, h.send(admin, http.MethodPost, "/api/admin/runners", body), http.StatusBadRequest, "bad_request")
	}

	runners := h.listRunners()
	if len(runners) != 3 {
		t.Fatalf("runners = %v, want local plus two", runners)
	}
	for _, r := range runners {
		requireKeys(t, r, "id", "name", "capabilities", "capacity", "active", "connected", "last_seen_at", "version", "revoked_at")
	}
	local := runners[0]
	if local["name"] != runner.LocalName || local["id"] != "" || local["connected"] != true || local["capacity"] != float64(3) {
		t.Errorf("local runner = %v", local)
	}
	lab := h.runnerNamed("lab-01")
	if lab["id"] != created["id"] || lab["connected"] != false || lab["capacity"] != float64(3) || lab["revoked_at"] != nil {
		t.Errorf("lab-01 = %v", lab)
	}
	if fmt.Sprint(lab["capabilities"]) != "[container]" {
		t.Errorf("default capabilities = %v", lab["capabilities"])
	}
	if kvm := h.runnerNamed("kvm-01"); fmt.Sprint(kvm["capabilities"]) != "[container vm]" || kvm["capacity"] != float64(1) {
		t.Errorf("kvm-01 = %v", kvm)
	}

	requireError(t, h.do(http.MethodGet, "/api/admin/runners", ""), http.StatusForbidden, "forbidden")
	requireError(t, h.do(http.MethodPost, "/api/admin/runners", `{"name":"lab-09"}`), http.StatusForbidden, "forbidden")
	requireError(t, h.send(admin, http.MethodDelete, "/api/admin/runners/nope", ""), http.StatusNotFound, "not_found")
}

func TestRemoteRunnerTakesAttemptsAndIsKickedOnRevoke(t *testing.T) {
	h := newHarnessWith(t, contenttest.Dir(), 1)
	created := h.createRunner(`{"name":"lab-01"}`)
	_, done := h.connectRunner("lab-01", created["token"].(string))
	waitUntil(t, "the runner to connect", func() bool { return h.runnerNamed("lab-01")["connected"] == true })

	lab := h.runnerNamed("lab-01")
	if lab["version"] != "test" || lab["capacity"] != float64(2) || lab["last_seen_at"] == nil {
		t.Errorf("connected runner = %v", lab)
	}

	summary := decodeArray(t, h.do(http.MethodGet, "/api/runners/summary", ""))
	if len(summary) != 2 {
		t.Fatalf("summary = %v", summary)
	}
	remote := summary[1].(map[string]any)
	requireKeys(t, remote, "name", "capabilities", "connected")
	if remote["name"] != "lab-01" || remote["connected"] != true {
		t.Errorf("summary entry = %v", remote)
	}

	clients := []*http.Client{h.client, h.login(h.other.Username, testPassword)}
	for _, name := range []string{"third", "fourth"} {
		h.addUser(name, testPassword, store.RoleUser)
		clients = append(clients, h.login(name, testPassword))
	}
	var placed []string
	var ids []string
	for _, client := range clients[:3] {
		resp := h.send(client, http.MethodPost, "/api/attempts", fmt.Sprintf(`{"lab_id":%q,"mode":"guided"}`, h.labs[0].Id))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("start: status = %d", resp.StatusCode)
		}
		body := decodeJSON(t, resp)
		placed = append(placed, body["runner"].(string))
		ids = append(ids, body["id"].(string))
	}
	if strings.Join(placed, ",") != "lab-01,local,lab-01" {
		t.Errorf("placements = %v, want lab-01, local, lab-01", placed)
	}
	for _, id := range ids {
		waitUntil(t, "the attempt to run", func() bool {
			view, err := h.attempts.Get(t.Context(), id)
			return err == nil && view.Status == store.StatusRunning
		})
	}

	resp := h.send(clients[3], http.MethodPost, "/api/attempts", fmt.Sprintf(`{"lab_id":%q,"mode":"guided"}`, h.labs[0].Id))
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("fourth start: status = %d, want 429", resp.StatusCode)
	}
	busy := decodeJSON(t, resp)
	if busy["sandboxes_active"] != float64(3) || busy["sandboxes_max"] != float64(3) {
		t.Errorf("busy = %v", busy)
	}
	if got := fmt.Sprint(busy["runners"]); got != "[map[active:2 capacity:2 name:lab-01] map[active:1 capacity:1 name:local]]" {
		t.Errorf("busy runners = %s", got)
	}

	stats := decodeJSON(t, h.send(h.adminClient(), http.MethodGet, "/api/admin/stats", ""))
	statRunners := stats["runners"].([]any)
	if len(statRunners) != 2 || statRunners[1].(map[string]any)["active"] != float64(2) {
		t.Errorf("stats runners = %v", statRunners)
	}

	if resp := h.send(h.adminClient(), http.MethodDelete, "/api/admin/runners/"+created["id"].(string), ""); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke: status = %d, want 204", resp.StatusCode)
	}
	select {
	case err := <-done:
		if !errors.Is(err, runnerclient.ErrUnauthorized) {
			t.Errorf("runner stopped with %v, want ErrUnauthorized", err)
		}
	case <-t.Context().Done():
		t.Fatal("the runner was not kicked")
	}

	for i, id := range ids {
		want := store.StatusError
		if placed[i] == runner.LocalName {
			want = store.StatusRunning
		}
		waitUntil(t, "the attempt on "+placed[i]+" to be "+want, func() bool {
			view, err := h.attempts.Get(t.Context(), id)
			return err == nil && view.Status == want
		})
		view, _ := h.attempts.Get(t.Context(), id)
		if want == store.StatusError && view.ErrorMessage != "runner disconnected" {
			t.Errorf("error message = %q", view.ErrorMessage)
		}
	}
	lab = h.runnerNamed("lab-01")
	if lab["connected"] != false || lab["revoked_at"] == nil || lab["active"] != float64(0) {
		t.Errorf("revoked runner = %v", lab)
	}
}

func TestRunnerSocketUsesTheBearerTokenNotTheSession(t *testing.T) {
	h := newHarness(t)
	h.createRunner(`{"name":"lab-01"}`)

	_, done := h.connectRunner("lab-01", "wrong-token")
	if err := <-done; !errors.Is(err, runnerclient.ErrUnauthorized) {
		t.Fatalf("runner with a wrong token stopped with %v, want ErrUnauthorized", err)
	}

	resp := h.send(h.anonymousClient(), http.MethodGet, "/ws/runner", "")
	if resp.StatusCode == http.StatusUnauthorized {
		t.Error("the runner socket went through the session check")
	}
	requireError(t, h.send(h.anonymousClient(), http.MethodGet, "/api/runners/summary", ""), http.StatusUnauthorized, "unauthorized")
}

func TestNoCapableRunnerIsAConflict(t *testing.T) {
	status, code := classify(fmt.Errorf("start: %w", runner.ErrNoRunner))
	if status != http.StatusConflict || code != "no_runner" {
		t.Errorf("classify = %d %s, want 409 no_runner", status, code)
	}
}
