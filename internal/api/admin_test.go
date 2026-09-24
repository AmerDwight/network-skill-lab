package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/AmerDwight/network-skill-lab/internal/content/contenttest"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

func (h *harness) adminClient() *http.Client {
	h.t.Helper()
	return h.login(h.admin.Username, testPassword)
}

func TestAdminEndpointsRejectPlainUsers(t *testing.T) {
	h := newHarness(t)
	tests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/admin/users", ""},
		{http.MethodPost, "/api/admin/users", `{"username":"newbie","password":"a good one","role":"user"}`},
		{http.MethodPatch, "/api/admin/users/" + h.other.ID, `{"role":"admin"}`},
		{http.MethodGet, "/api/admin/attempts", ""},
		{http.MethodGet, "/api/admin/stats", ""},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			requireError(t, h.do(tt.method, tt.path, tt.body), http.StatusForbidden, "forbidden")
		})
	}
}

func TestAdminListsUsers(t *testing.T) {
	h := newHarness(t)
	users := decodeArray(t, h.send(h.adminClient(), http.MethodGet, "/api/admin/users", ""))
	if len(users) != 4 {
		t.Fatalf("got %d users, want 4 (local plus three fixtures)", len(users))
	}
	requireKeys(t, users[0].(map[string]any), "id", "username", "role", "locale", "created_at", "disabled_at", "attempts")
}

func TestAdminCreatesAUser(t *testing.T) {
	h := newHarness(t)
	admin := h.adminClient()

	resp := h.send(admin, http.MethodPost, "/api/admin/users",
		`{"username":"newbie","password":"a good one","role":"admin"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	body := decodeJSON(t, resp)
	if body["username"] != "newbie" || body["role"] != store.RoleAdmin || body["attempts"] != float64(0) {
		t.Errorf("created user = %v", body)
	}

	requireError(t, h.send(admin, http.MethodPost, "/api/admin/users",
		`{"username":"newbie","password":"another one","role":"user"}`), http.StatusConflict, "username_taken")
	requireError(t, h.send(admin, http.MethodPost, "/api/admin/users",
		`{"username":"NO","password":"a good one","role":"user"}`), http.StatusBadRequest, "bad_request")
	requireError(t, h.send(admin, http.MethodPost, "/api/admin/users",
		`{"username":"shorty","password":"short","role":"user"}`), http.StatusBadRequest, "bad_request")

	if _, err := h.store.Users.ByUsername(t.Context(), "newbie"); err != nil {
		t.Fatalf("created user is not in the store: %v", err)
	}
}

func TestAdminCannotPatchItself(t *testing.T) {
	h := newHarness(t)
	requireError(t, h.send(h.adminClient(), http.MethodPatch, "/api/admin/users/"+h.admin.ID, `{"disabled":true}`),
		http.StatusBadRequest, "cannot_modify_self")
}

func TestAdminPatchesAnUnknownUser(t *testing.T) {
	h := newHarness(t)
	requireError(t, h.send(h.adminClient(), http.MethodPatch, "/api/admin/users/nobody", `{"role":"admin"}`),
		http.StatusNotFound, "not_found")
}

func TestAdminChangesRoleAndPassword(t *testing.T) {
	h := newHarness(t)
	admin := h.adminClient()

	body := decodeJSON(t, h.send(admin, http.MethodPatch, "/api/admin/users/"+h.other.ID,
		`{"role":"admin","password":"a brand new one"}`))
	if body["role"] != store.RoleAdmin {
		t.Errorf("role = %v, want admin", body["role"])
	}
	h.login(h.other.Username, "a brand new one")

	requireError(t, h.send(admin, http.MethodPatch, "/api/admin/users/"+h.other.ID, `{"role":"root"}`),
		http.StatusBadRequest, "bad_request")
	requireError(t, h.send(admin, http.MethodPatch, "/api/admin/users/"+h.other.ID, `{"password":"tiny"}`),
		http.StatusBadRequest, "bad_request")
}

func TestAdminDisableDropsTheUsersSessions(t *testing.T) {
	h := newHarness(t)
	victim := h.login(h.other.Username, testPassword)
	if resp := h.send(victim, http.MethodGet, "/api/auth/me", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("status before disabling = %d", resp.StatusCode)
	}

	body := decodeJSON(t, h.send(h.adminClient(), http.MethodPatch, "/api/admin/users/"+h.other.ID, `{"disabled":true}`))
	if body["disabled_at"] == nil {
		t.Errorf("disabled_at = nil, want a timestamp")
	}

	resp := h.send(victim, http.MethodGet, "/api/auth/me", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status after disabling = %d, want 401", resp.StatusCode)
	}
	requireError(t, resp, http.StatusUnauthorized, "unauthorized")
}

func TestAdminStats(t *testing.T) {
	h := newHarness(t)
	h.running()

	body := decodeJSON(t, h.send(h.adminClient(), http.MethodGet, "/api/admin/stats", ""))
	requireKeys(t, body, "sandboxes_active", "sandboxes_max", "recordings_bytes", "attempts")
	if body["sandboxes_active"] != float64(1) {
		t.Errorf("sandboxes_active = %v, want 1", body["sandboxes_active"])
	}
	if body["sandboxes_max"] != float64(defaultMaxSandboxes) {
		t.Errorf("sandboxes_max = %v, want %d", body["sandboxes_max"], defaultMaxSandboxes)
	}
	if body["attempts"] != float64(1) {
		t.Errorf("attempts = %v, want 1", body["attempts"])
	}
	if body["recordings_bytes"] != float64(0) {
		t.Errorf("recordings_bytes = %v, want 0", body["recordings_bytes"])
	}
}

func TestAdminListsActiveAttemptsOfEveryone(t *testing.T) {
	h := newHarness(t)
	mine := h.running()

	attempts := decodeArray(t, h.send(h.adminClient(), http.MethodGet, "/api/admin/attempts", ""))
	if len(attempts) != 1 {
		t.Fatalf("got %d active attempts, want 1", len(attempts))
	}
	first := attempts[0].(map[string]any)
	requireKeys(t, first, "id", "lab", "mode", "status", "elapsed_ms", "created_at", "user")
	if first["id"] != mine {
		t.Errorf("id = %v, want %s", first["id"], mine)
	}
	user := first["user"].(map[string]any)
	if user["id"] != h.user.ID || user["username"] != testUsername {
		t.Errorf("user = %v", user)
	}
}

func TestAdminAbandonsAnotherUsersAttempt(t *testing.T) {
	h := newHarness(t)
	id := h.running()

	body := decodeJSON(t, h.send(h.adminClient(), http.MethodPost, "/api/admin/attempts/"+id+"/abandon", ""))
	if body["status"] != store.StatusAbandoned {
		t.Fatalf("status = %v, want abandoned", body["status"])
	}
	requireError(t, h.send(h.adminClient(), http.MethodPost, "/api/admin/attempts/"+id+"/abandon", ""),
		http.StatusConflict, "attempt_finished")
	requireError(t, h.send(h.adminClient(), http.MethodPost, "/api/admin/attempts/nope/abandon", ""),
		http.StatusNotFound, "not_found")
}

func TestAdminDeletesATerminalAttemptWithItsFiles(t *testing.T) {
	h := newHarness(t)
	admin := h.adminClient()
	id := h.running()

	requireError(t, h.send(admin, http.MethodDelete, "/api/admin/attempts/"+id, ""),
		http.StatusConflict, "attempt_running")

	dir := filepath.Join(h.dataDir, "recordings", id)
	h.writeCast(id, "web01", "main")

	if _, err := h.attempts.AbandonAsAdmin(t.Context(), id); err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if resp := h.send(admin, http.MethodDelete, "/api/admin/attempts/"+id, ""); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if _, err := h.store.Attempts.Get(t.Context(), id); err == nil {
		t.Error("the attempt row is still in the store")
	}
	if entries, err := h.store.CommandLog.ListByAttempt(t.Context(), id); err != nil || len(entries) != 0 {
		t.Errorf("command log = %v (%v), want it gone with the attempt", entries, err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("recordings directory %s still exists (%v)", dir, err)
	}
	requireError(t, h.send(admin, http.MethodDelete, "/api/admin/attempts/"+id, ""), http.StatusNotFound, "not_found")
}

func TestStartIsRefusedWhenEverySandboxIsTaken(t *testing.T) {
	h := newHarnessWith(t, contenttest.Dir(), 1)
	h.running()

	other := h.login(h.other.Username, testPassword)
	resp := h.send(other, http.MethodPost, "/api/attempts",
		fmt.Sprintf(`{"lab_id":%q,"mode":"guided"}`, h.labs[0].Id))
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", resp.StatusCode)
	}
	body := decodeJSON(t, resp)
	requireKeys(t, body, "error", "sandboxes_active", "sandboxes_max")
	if body["sandboxes_active"] != float64(1) || body["sandboxes_max"] != float64(1) {
		t.Errorf("body = %v", body)
	}
	if failure := body["error"].(map[string]any); failure["code"] != "runner_busy" {
		t.Errorf("error.code = %v, want runner_busy", failure["code"])
	}
}
