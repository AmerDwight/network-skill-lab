package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

func (h *harness) seedAttempt(userID, labID string, createdAt time.Time, status string) string {
	h.t.Helper()
	lab, ok := h.labByID(labID)
	if !ok {
		h.t.Fatalf("unknown lab %s", labID)
	}
	resolved, err := lab.ResolveFor(1)
	if err != nil {
		h.t.Fatalf("resolve lab %s: %v", labID, err)
	}
	params, err := json.Marshal(resolved.Params)
	if err != nil {
		h.t.Fatalf("encode params: %v", err)
	}

	ended := createdAt.Add(time.Minute)
	att := store.Attempt{
		ID:         store.NewID(),
		UserID:     userID,
		LabID:      labID,
		LabVersion: lab.Version,
		CaseID:     resolved.CaseID,
		Mode:       "guided",
		ParamsJSON: string(params),
		Status:     status,
		StartedAt:  &createdAt,
		EndedAt:    &ended,
		ElapsedMS:  60000,
		CreatedAt:  createdAt,
	}
	if err := h.store.Attempts.Create(h.t.Context(), att); err != nil {
		h.t.Fatalf("seed attempt: %v", err)
	}
	return att.ID
}

func (h *harness) labByID(id string) (content.Lab, bool) {
	for _, lab := range h.labs {
		if lab.Id == id {
			return lab, true
		}
	}
	return content.Lab{}, false
}

func TestOwnershipHidesOtherUsersAttempts(t *testing.T) {
	h := newHarness(t)
	id := h.running()
	other := h.login(h.other.Username, testPassword)

	for _, path := range []string{"", "/result", "/commands", "/recordings"} {
		t.Run("GET "+path, func(t *testing.T) {
			requireError(t, h.send(other, http.MethodGet, "/api/attempts/"+id+path, ""),
				http.StatusNotFound, "not_found")
		})
	}
	requireError(t, h.send(other, http.MethodPost, "/api/attempts/"+id+"/abandon", ""),
		http.StatusNotFound, "not_found")
	requireError(t, h.send(other, http.MethodPost, "/api/attempts/"+id+"/submit", ""),
		http.StatusNotFound, "not_found")
}

func TestOwnershipHidesOtherUsersSockets(t *testing.T) {
	h := newHarness(t)
	id := h.running()
	other := h.login(h.other.Username, testPassword)

	for _, path := range []string{"/events", "/term/web01/main"} {
		t.Run(path, func(t *testing.T) {
			conn, resp, err := dialAs(t, h, other, "/ws/attempts/"+id+path)
			if err == nil {
				_ = conn.CloseNow()
				t.Fatal("dial succeeded on another user's attempt")
			}
			requireError(t, resp, http.StatusNotFound, "not_found")
		})
	}
}

func TestAdminSeesTerminalAttemptsOfOthersButNotRunningOnes(t *testing.T) {
	h := newHarness(t)
	admin := h.adminClient()
	id := h.running()

	requireError(t, h.send(admin, http.MethodGet, "/api/attempts/"+id, ""), http.StatusNotFound, "not_found")

	if _, err := h.attempts.AbandonAsAdmin(t.Context(), id); err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if resp := h.send(admin, http.MethodGet, "/api/attempts/"+id, ""); resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 on a terminal attempt", resp.StatusCode)
	}
	if resp := h.send(admin, http.MethodGet, "/api/attempts/"+id+"/result", ""); resp.StatusCode != http.StatusOK {
		t.Errorf("result status = %d, want 200", resp.StatusCode)
	}
}

func TestAdminCannotOpenSocketsOfOthers(t *testing.T) {
	h := newHarness(t)
	admin := h.adminClient()
	id := h.running()

	conn, resp, err := dialAs(t, h, admin, "/ws/attempts/"+id+"/events")
	if err == nil {
		_ = conn.CloseNow()
		t.Fatal("an admin opened another user's events socket")
	}
	requireError(t, resp, http.StatusNotFound, "not_found")
}

func TestAdminCannotAbandonOrSubmitThroughTheOwnerRoutes(t *testing.T) {
	h := newHarness(t)
	admin := h.adminClient()
	id := h.running()

	requireError(t, h.send(admin, http.MethodPost, "/api/attempts/"+id+"/abandon", ""),
		http.StatusNotFound, "not_found")
	requireError(t, h.send(admin, http.MethodPost, "/api/attempts/"+id+"/submit", ""),
		http.StatusNotFound, "not_found")
}

func TestHistoryReturnsTheCallersAttempts(t *testing.T) {
	h := newHarness(t)
	base := time.Now().Add(-time.Hour)
	mine := h.seedAttempt(h.user.ID, h.labs[0].Id, base, store.StatusPassed)
	h.seedAttempt(h.other.ID, h.labs[0].Id, base.Add(time.Minute), store.StatusAbandoned)

	history := decodeArray(t, h.do(http.MethodGet, "/api/history", ""))
	if len(history) != 1 {
		t.Fatalf("got %d items, want only the caller's", len(history))
	}
	item := history[0].(map[string]any)
	requireKeys(t, item, "id", "lab", "mode", "status", "elapsed_ms", "submit_count",
		"command_count", "created_at", "ended_at", "user")
	if item["id"] != mine {
		t.Errorf("id = %v, want %s", item["id"], mine)
	}
	if user := item["user"].(map[string]any); user["username"] != testUsername {
		t.Errorf("user = %v", user)
	}
}

func TestHistoryPaginatesWithLimitAndBefore(t *testing.T) {
	h := newHarness(t)
	base := time.Now().Add(-time.Hour).UTC()
	for i := range 5 {
		h.seedAttempt(h.user.ID, h.labs[0].Id, base.Add(time.Duration(i)*time.Minute), store.StatusPassed)
	}

	page := decodeArray(t, h.do(http.MethodGet, "/api/history?limit=2", ""))
	if len(page) != 2 {
		t.Fatalf("got %d items, want 2", len(page))
	}
	last := page[1].(map[string]any)["created_at"].(string)

	next := decodeArray(t, h.do(http.MethodGet, "/api/history?limit=2&before="+last, ""))
	if len(next) != 2 {
		t.Fatalf("got %d items on the second page, want 2", len(next))
	}
	if next[0].(map[string]any)["id"] == page[1].(map[string]any)["id"] {
		t.Error("the second page repeats the cursor row")
	}

	if capped := decodeArray(t, h.do(http.MethodGet, "/api/history?limit=9999", "")); len(capped) != 5 {
		t.Errorf("got %d items with an oversized limit, want 5", len(capped))
	}
	requireError(t, h.do(http.MethodGet, "/api/history?limit=0", ""), http.StatusBadRequest, "bad_request")
	requireError(t, h.do(http.MethodGet, "/api/history?before=yesterday", ""), http.StatusBadRequest, "bad_request")
}

func TestHistoryUserIDIsAdminOnly(t *testing.T) {
	h := newHarness(t)
	base := time.Now().Add(-time.Hour)
	theirs := h.seedAttempt(h.other.ID, h.labs[0].Id, base, store.StatusPassed)

	requireError(t, h.do(http.MethodGet, "/api/history?user_id="+h.other.ID, ""), http.StatusForbidden, "forbidden")

	admin := h.adminClient()
	history := decodeArray(t, h.send(admin, http.MethodGet, "/api/history?user_id="+h.other.ID, ""))
	if len(history) != 1 || history[0].(map[string]any)["id"] != theirs {
		t.Fatalf("admin history = %v", history)
	}
	if user := history[0].(map[string]any)["user"].(map[string]any); user["username"] != h.other.Username {
		t.Errorf("user = %v", user)
	}
	requireError(t, h.send(admin, http.MethodGet, "/api/history?user_id=nobody", ""), http.StatusNotFound, "not_found")
}

func TestAttemptCommandsPaginate(t *testing.T) {
	h := newHarness(t)
	id := h.running()

	entries := make([]store.CommandEntry, 0, 3)
	for i := range 3 {
		entries = append(entries, store.CommandEntry{
			AttemptID: id,
			Node:      "web01",
			TS:        time.Now().Add(time.Duration(i) * time.Second),
			User:      "student",
			CWD:       "/home/student",
			Command:   fmt.Sprintf("ip addr show %d", i),
			ExitCode:  0,
		})
	}
	if err := h.store.CommandLog.AppendBatch(t.Context(), entries); err != nil {
		t.Fatalf("append commands: %v", err)
	}

	all := decodeArray(t, h.do(http.MethodGet, "/api/attempts/"+id+"/commands", ""))
	if len(all) != 3 {
		t.Fatalf("got %d commands, want 3", len(all))
	}
	requireKeys(t, all[0].(map[string]any), "id", "node", "ts", "user", "cwd", "command", "exit_code")

	first := all[0].(map[string]any)
	rest := decodeArray(t, h.do(http.MethodGet, fmt.Sprintf("/api/attempts/%s/commands?after=%s", id, first["id"].(string)), ""))
	if len(rest) != 2 {
		t.Fatalf("got %d commands after the first, want 2", len(rest))
	}
	if one := decodeArray(t, h.do(http.MethodGet, "/api/attempts/"+id+"/commands?limit=1", "")); len(one) != 1 {
		t.Errorf("got %d commands with limit=1", len(one))
	}
	requireError(t, h.do(http.MethodGet, "/api/attempts/"+id+"/commands?after=soon", ""),
		http.StatusBadRequest, "bad_request")
}

func TestAttemptRecordingsAndCast(t *testing.T) {
	h := newHarness(t)
	id := h.running()
	rec := h.writeCast(id, "web01", "main")

	list := decodeArray(t, h.do(http.MethodGet, "/api/attempts/"+id+"/recordings", ""))
	if len(list) != 1 {
		t.Fatalf("got %d recordings, want 1", len(list))
	}
	first := list[0].(map[string]any)
	requireKeys(t, first, "id", "node", "tab", "started_at", "ended_at", "bytes")
	if first["id"] != rec.ID || first["tab"] != "main" {
		t.Errorf("recording = %v", first)
	}
	if first["bytes"].(float64) <= 0 {
		t.Errorf("bytes = %v, want the file size", first["bytes"])
	}

	resp := h.do(http.MethodGet, "/api/attempts/"+id+"/recordings/"+rec.ID+"/cast", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cast status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != castContentType {
		t.Errorf("Content-Type = %q, want %q", got, castContentType)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read cast: %v", err)
	}
	if resp.Header.Get("Content-Length") != fmt.Sprint(len(raw)) {
		t.Errorf("Content-Length = %q, body is %d bytes", resp.Header.Get("Content-Length"), len(raw))
	}
	if !strings.HasPrefix(string(raw), `{"version":2`) {
		t.Errorf("cast does not start with an asciicast v2 header: %q", string(raw))
	}
	if !strings.Contains(string(raw), "hello from web01") {
		t.Error("cast has no output event")
	}

	requireError(t, h.do(http.MethodGet, "/api/attempts/"+id+"/recordings/nope/cast", ""),
		http.StatusNotFound, "not_found")

	if err := os.Remove(rec.Path); err != nil {
		t.Fatalf("remove cast file: %v", err)
	}
	requireError(t, h.do(http.MethodGet, "/api/attempts/"+id+"/recordings/"+rec.ID+"/cast", ""),
		http.StatusNotFound, "not_found")
}

func TestAdminReadsRecordingsOfATerminalAttempt(t *testing.T) {
	h := newHarness(t)
	id := h.running()
	rec := h.writeCast(id, "web01", "main")
	admin := h.adminClient()

	requireError(t, h.send(admin, http.MethodGet, "/api/attempts/"+id+"/recordings/"+rec.ID+"/cast", ""),
		http.StatusNotFound, "not_found")

	if _, err := h.attempts.AbandonAsAdmin(t.Context(), id); err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if resp := h.send(admin, http.MethodGet, "/api/attempts/"+id+"/recordings/"+rec.ID+"/cast", ""); resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestAttemptEndpointsReportUnknownIDsAsNotFound(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"", "/result", "/commands", "/recordings", "/recordings/x/cast"} {
		t.Run(path, func(t *testing.T) {
			requireError(t, h.do(http.MethodGet, "/api/attempts/ghost"+path, ""), http.StatusNotFound, "not_found")
		})
	}
}
