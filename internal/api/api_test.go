package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func decodeJSON(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var body map[string]any
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode body %q: %v", raw, err)
	}
	return body
}

func decodeArray(t *testing.T, resp *http.Response) []any {
	t.Helper()
	var body []any
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode body %q: %v", raw, err)
	}
	return body
}

func requireError(t *testing.T, resp *http.Response, status int, code string) {
	t.Helper()
	if resp.StatusCode != status {
		t.Fatalf("status = %d, want %d", resp.StatusCode, status)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	body := decodeJSON(t, resp)
	failure, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("body %v has no error object", body)
	}
	if failure["code"] != code {
		t.Errorf("error.code = %v, want %q", failure["code"], code)
	}
	if message, _ := failure["message"].(string); message == "" {
		t.Error("error.message is empty")
	}
}

func requireKeys(t *testing.T, body map[string]any, want ...string) {
	t.Helper()
	got := make([]string, 0, len(body))
	for key := range body {
		got = append(got, key)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("fields = %v, want %v", got, want)
	}
}

func TestHealth(t *testing.T) {
	h := newHarness(t)
	resp := h.do(http.MethodGet, "/api/health", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := decodeJSON(t, resp)
	requireKeys(t, body, "ok", "docker", "image", "image_name", "mem_available_mb", "error")
	if body["ok"] != true || body["image_name"] != "nsl/node" {
		t.Errorf("health = %v", body)
	}
}

func TestListLabs(t *testing.T) {
	h := newHarness(t)

	resp := h.do(http.MethodGet, "/api/labs", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	labs := decodeArray(t, resp)
	if len(labs) != len(h.labs) {
		t.Fatalf("got %d labs, want %d", len(labs), len(h.labs))
	}
	lab := labs[0].(map[string]any)
	requireKeys(t, lab, "id", "title", "topic", "level", "modes", "estimated_minutes")
	if lab["title"] != h.labs[0].Title.Zh {
		t.Errorf("title = %v, want the zh title", lab["title"])
	}

	english := decodeArray(t, h.do(http.MethodGet, "/api/labs?lang=en", ""))[0].(map[string]any)
	if english["title"] != h.labs[0].Title.En {
		t.Errorf("title with lang=en = %v, want the en title", english["title"])
	}
}

func TestLabLanguages(t *testing.T) {
	h := newHarness(t)
	tests := []struct {
		query string
		want  string
	}{
		{"", h.labs[0].Title.Zh},
		{"?lang=zh", h.labs[0].Title.Zh},
		{"?lang=zh-TW", h.labs[0].Title.Zh},
		{"?lang=en", h.labs[0].Title.En},
		{"?lang=fr", h.labs[0].Title.Zh},
	}
	for _, tt := range tests {
		t.Run("lang"+tt.query, func(t *testing.T) {
			body := decodeJSON(t, h.do(http.MethodGet, "/api/labs/"+h.labs[0].Id+tt.query, ""))
			if body["title"] != tt.want {
				t.Errorf("title = %v, want %q", body["title"], tt.want)
			}
		})
	}
}

func TestGetLab(t *testing.T) {
	h := newHarness(t)
	resp := h.do(http.MethodGet, "/api/labs/"+h.labs[0].Id, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := decodeJSON(t, resp)
	requireKeys(t, body, "id", "title", "topic", "level", "modes", "estimated_minutes", "nodes", "checkpoints")

	nodes := body["nodes"].([]any)
	if len(nodes) == 0 {
		t.Fatal("lab has no nodes")
	}
	names := make([]string, 0, len(nodes))
	for _, raw := range nodes {
		node := raw.(map[string]any)
		requireKeys(t, node, "name", "role")
		names = append(names, node["name"].(string))
	}
	if !slices.IsSorted(names) {
		t.Errorf("nodes = %v, want them sorted by name", names)
	}

	checkpoints := body["checkpoints"].([]any)
	if len(checkpoints) != 2 {
		t.Fatalf("got %d checkpoints, want 2", len(checkpoints))
	}
	requireKeys(t, checkpoints[0].(map[string]any), "id", "title")
}

func TestGetLabUnknown(t *testing.T) {
	h := newHarness(t)
	requireError(t, h.do(http.MethodGet, "/api/labs/nope", ""), http.StatusNotFound, "unknown_lab")
}

func TestUnknownRoute(t *testing.T) {
	h := newHarness(t)
	requireError(t, h.do(http.MethodGet, "/api/nope", ""), http.StatusNotFound, "not_found")
}

func TestCreateAttempt(t *testing.T) {
	h := newHarness(t)

	resp := h.do(http.MethodPost, "/api/attempts", `{"lab_id":"`+h.labs[0].Id+`","mode":"guided"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	body := decodeJSON(t, resp)
	requireKeys(t, body,
		"id", "lab_id", "mode", "status", "error_message", "lab", "ticket", "nodes",
		"checkpoints", "elapsed_ms", "started_at", "ended_at", "server_time", "created_at")

	if body["status"] != "provisioning" && body["status"] != "running" {
		t.Errorf("status = %v", body["status"])
	}
	if body["error_message"] != "" {
		t.Errorf("error_message = %v, want an empty string", body["error_message"])
	}
	if body["started_at"] != nil && body["status"] == "provisioning" {
		t.Errorf("started_at = %v, want null while provisioning", body["started_at"])
	}
	if body["ended_at"] != nil {
		t.Errorf("ended_at = %v, want null", body["ended_at"])
	}
	if ticket := body["ticket"].(string); strings.Contains(ticket, "{{") {
		t.Errorf("ticket still has placeholders: %q", ticket)
	}
	requireKeys(t, body["lab"].(map[string]any), "id", "title", "topic", "level", "modes", "estimated_minutes")

	checkpoints := body["checkpoints"].([]any)
	if len(checkpoints) != 2 {
		t.Fatalf("got %d checkpoints, want 2", len(checkpoints))
	}
	first := checkpoints[0].(map[string]any)
	requireKeys(t, first, "id", "title", "status", "first_passed_at")
	if first["status"] != "pending" || first["first_passed_at"] != nil {
		t.Errorf("checkpoint = %v, want a pending one without first_passed_at", first)
	}
	if server, ok := body["server_time"].(string); !ok || !strings.HasSuffix(server, "Z") || len(server) != len("2026-09-23T10:00:00.000Z") {
		t.Errorf("server_time = %v, want RFC 3339 UTC with milliseconds", body["server_time"])
	}
}

func TestCreateAttemptErrors(t *testing.T) {
	h := newHarness(t)
	tests := []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{"bad json", `{`, http.StatusBadRequest, "bad_request"},
		{"unknown lab", `{"lab_id":"nope","mode":"guided"}`, http.StatusNotFound, "unknown_lab"},
		{"bad mode", `{"lab_id":"` + h.labs[0].Id + `","mode":"freeform"}`, http.StatusBadRequest, "mode_not_allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireError(t, h.do(http.MethodPost, "/api/attempts", tt.body), tt.status, tt.code)
		})
	}
}

func TestCreateAttemptTwiceConflicts(t *testing.T) {
	h := newHarness(t)
	h.start()
	requireError(t,
		h.do(http.MethodPost, "/api/attempts", `{"lab_id":"`+h.labs[0].Id+`","mode":"guided"}`),
		http.StatusConflict, "attempt_active")
}

func TestCurrentAttempt(t *testing.T) {
	h := newHarness(t)

	empty := h.do(http.MethodGet, "/api/attempts/current", "")
	if empty.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", empty.StatusCode, http.StatusNoContent)
	}

	id := h.start()
	resp := h.do(http.MethodGet, "/api/attempts/current", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if body := decodeJSON(t, resp); body["id"] != id {
		t.Errorf("id = %v, want %q", body["id"], id)
	}

	h.do(http.MethodPost, "/api/attempts/"+id+"/abandon", "")
	after := h.do(http.MethodGet, "/api/attempts/current", "")
	if after.StatusCode != http.StatusNoContent {
		t.Errorf("status after abandoning = %d, want %d", after.StatusCode, http.StatusNoContent)
	}
}

func TestGetAttemptUnknown(t *testing.T) {
	h := newHarness(t)
	requireError(t, h.do(http.MethodGet, "/api/attempts/01NOPE", ""), http.StatusNotFound, "not_found")
}

func TestAbandonAttempt(t *testing.T) {
	h := newHarness(t)
	id := h.running()

	resp := h.do(http.MethodPost, "/api/attempts/"+id+"/abandon", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := decodeJSON(t, resp)
	if body["status"] != "abandoned" {
		t.Errorf("status = %v, want abandoned", body["status"])
	}
	if body["ended_at"] == nil {
		t.Error("ended_at is null after abandoning")
	}

	requireError(t, h.do(http.MethodPost, "/api/attempts/"+id+"/abandon", ""), http.StatusConflict, "attempt_finished")
	requireError(t, h.do(http.MethodPost, "/api/attempts/01NOPE/abandon", ""), http.StatusNotFound, "not_found")
}

func TestResult(t *testing.T) {
	h := newHarness(t)
	id := h.running()

	requireError(t, h.do(http.MethodGet, "/api/attempts/"+id+"/result", ""), http.StatusConflict, "attempt_running")

	h.do(http.MethodPost, "/api/attempts/"+id+"/abandon", "")

	resp := h.do(http.MethodGet, "/api/attempts/"+id+"/result", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := decodeJSON(t, resp)
	requireKeys(t, body, "attempt_id", "status", "lab", "elapsed_ms", "command_count", "checkpoints", "solution")
	if body["attempt_id"] != id || body["status"] != "abandoned" {
		t.Errorf("result = %v", body)
	}
	if body["command_count"] != float64(0) {
		t.Errorf("command_count = %v, want 0", body["command_count"])
	}
	solution := body["solution"].(string)
	if solution == "" {
		t.Error("solution is empty")
	}
	checkpoint := body["checkpoints"].([]any)[0].(map[string]any)
	if checkpoint["first_passed_at"] != nil {
		t.Errorf("first_passed_at = %v, want null", checkpoint["first_passed_at"])
	}

	english := decodeJSON(t, h.do(http.MethodGet, "/api/attempts/"+id+"/result?lang=en", ""))
	if english["solution"] == solution {
		t.Error("the en solution is the same as the zh one")
	}

	requireError(t, h.do(http.MethodGet, "/api/attempts/01NOPE/result", ""), http.StatusNotFound, "not_found")
}

func TestInternalErrorMessageIsGeneric(t *testing.T) {
	const want = `{"error":{"code":"internal","message":"internal error"}}` + "\n"

	t.Run("through a handler", func(t *testing.T) {
		h := newHarness(t)
		if err := h.store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}

		resp := h.do(http.MethodGet, "/api/attempts/current", "")
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
		}
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if string(raw) != want {
			t.Errorf("body = %q, want %q", raw, want)
		}
	})

	t.Run("directly", func(t *testing.T) {
		s := &server{log: discardLogger()}
		rec := httptest.NewRecorder()

		s.fail(rec, "01ATTEMPT", errors.New("open /var/lib/nsl/secret.db: permission denied"))

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
		}
		body := rec.Body.String()
		if body != want {
			t.Errorf("body = %q, want %q", body, want)
		}
		if strings.Contains(body, "secret.db") || strings.Contains(body, "permission denied") {
			t.Errorf("body %q leaks the original error", body)
		}
	})
}
