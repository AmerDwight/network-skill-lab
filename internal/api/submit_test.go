package api

import (
	"net/http"
	"testing"

	"github.com/AmerDwight/network-skill-lab/internal/store"
)

func TestAttemptPayloadPerMode(t *testing.T) {
	h := newContentHarness(t)

	tutorial := decodeJSON(t, h.do(http.MethodGet, "/api/attempts/"+h.runningLab("net-ip-02-modes", "tutorial"), ""))
	if tutorial["checkpoints_hidden"] != false {
		t.Errorf("tutorial checkpoints_hidden = %v", tutorial["checkpoints_hidden"])
	}
	if tutorial["submit_count"] != float64(0) {
		t.Errorf("tutorial submit_count = %v", tutorial["submit_count"])
	}
	steps := tutorial["tutorial_steps"].([]any)
	if len(steps) != 1 {
		t.Fatalf("tutorial_steps = %v, want one", steps)
	}
	step := steps[0].(map[string]any)
	requireKeys(t, step, "checkpoint", "instruction")
	if step["checkpoint"] != "link-up" || step["instruction"] != "執行 ip link set eth1 up" {
		t.Errorf("step = %v", step)
	}
	if ids := checkpointIDs(tutorial["checkpoints"].([]any)); len(ids) != 1 {
		t.Errorf("tutorial checkpoints = %v, want the visible one", ids)
	}
	if _, err := h.attempts.AbandonAsAdmin(t.Context(), tutorial["id"].(string)); err != nil {
		t.Fatalf("abandon: %v", err)
	}

	guided := decodeJSON(t, h.do(http.MethodGet, "/api/attempts/"+h.runningLab("net-ip-03-submit", "guided"), ""))
	if guided["tutorial_steps"] != nil {
		t.Errorf("guided tutorial_steps = %v, want null", guided["tutorial_steps"])
	}
	if guided["checkpoints_hidden"] != false || len(guided["checkpoints"].([]any)) != 1 {
		t.Errorf("guided payload = %v", guided)
	}
	if _, err := h.attempts.AbandonAsAdmin(t.Context(), guided["id"].(string)); err != nil {
		t.Fatalf("abandon: %v", err)
	}

	real := decodeJSON(t, h.do(http.MethodGet, "/api/attempts/"+h.runningLab("net-ip-03-submit", "real"), ""))
	if real["checkpoints_hidden"] != true {
		t.Errorf("real checkpoints_hidden = %v, want true", real["checkpoints_hidden"])
	}
	if entries := real["checkpoints"].([]any); len(entries) != 0 {
		t.Errorf("real checkpoints = %v, want empty", entries)
	}
	if real["tutorial_steps"] != nil {
		t.Errorf("real tutorial_steps = %v, want null", real["tutorial_steps"])
	}
}

func TestSubmitRevealsResultAndFinishes(t *testing.T) {
	h := newContentHarness(t)
	id := h.runningLab("net-ip-03-submit", "real")

	events := dial(t, h, "/ws/attempts/"+id+"/events")
	if message := readMessage(t, events); message["type"] != "status" {
		t.Fatalf("first message = %v, want the status snapshot", message)
	}

	failed := h.submit(id)
	requireKeys(t, failed, "passed", "checkpoints", "hidden_failed", "submit_count")
	if failed["passed"] != false || failed["hidden_failed"] != float64(1) || failed["submit_count"] != float64(1) {
		t.Errorf("first submit = %v", failed)
	}
	checkpoints := failed["checkpoints"].([]any)
	if len(checkpoints) != 1 {
		t.Fatalf("checkpoints = %v, want only the visible one", checkpoints)
	}
	requireKeys(t, checkpoints[0].(map[string]any), "id", "title", "status")
	if checkpoints[0].(map[string]any)["status"] != store.CheckpointFail {
		t.Errorf("checkpoint = %v, want fail", checkpoints[0])
	}

	message := waitForMessage(t, events, "submit")
	if message["passed"] != false || message["hidden_failed"] != float64(1) || message["submit_count"] != float64(1) {
		t.Errorf("submit event = %v", message)
	}

	if body := decodeJSON(t, h.do(http.MethodGet, "/api/attempts/"+id, "")); body["submit_count"] != float64(1) {
		t.Errorf("submit_count = %v after one submit", body["submit_count"])
	}

	h.runner.passChecks()
	passed := h.submit(id)
	if passed["passed"] != true || passed["hidden_failed"] != float64(0) || passed["submit_count"] != float64(2) {
		t.Errorf("second submit = %v", passed)
	}

	result := decodeJSON(t, h.do(http.MethodGet, "/api/attempts/"+id+"/result", ""))
	if result["status"] != store.StatusPassed {
		t.Errorf("status = %v, want passed", result["status"])
	}
	if result["submit_count"] != float64(2) {
		t.Errorf("result submit_count = %v", result["submit_count"])
	}
}

func TestSubmitSendsNoCheckpointEvents(t *testing.T) {
	h := newContentHarness(t)
	id := h.runningLab("net-ip-03-submit", "real")

	events := dial(t, h, "/ws/attempts/"+id+"/events")
	if message := readMessage(t, events); message["type"] != "status" {
		t.Fatalf("first message = %v, want the status snapshot", message)
	}

	h.runner.passChecks()
	if result := h.submit(id); result["passed"] != true {
		t.Fatalf("submit = %v, want passed", result)
	}
	for range 8 {
		message := readMessage(t, events)
		if message["type"] == "checkpoint" {
			t.Fatalf("a checkpoint event reached a real-mode attempt: %v", message)
		}
		if message["type"] == "status" && message["status"] == store.StatusPassed {
			return
		}
	}
	t.Fatal("the attempt never reported passed on the events socket")
}

func TestSubmitErrors(t *testing.T) {
	h := newContentHarness(t)

	guided := h.runningLab("net-ip-03-submit", "guided")
	requireError(t, h.do(http.MethodPost, "/api/attempts/"+guided+"/submit", ""), http.StatusBadRequest, "mode_not_real")
	if _, err := h.attempts.AbandonAsAdmin(t.Context(), guided); err != nil {
		t.Fatalf("abandon: %v", err)
	}

	release := h.runner.hold()
	provisioning := h.startLab("net-ip-03-submit", "real")
	requireError(t, h.do(http.MethodPost, "/api/attempts/"+provisioning+"/submit", ""), http.StatusConflict, "attempt_provisioning")
	release()
	waitUntil(t, "the attempt to be running", func() bool {
		view, err := h.attempts.Get(t.Context(), provisioning)
		return err == nil && view.Status == store.StatusRunning
	})

	if _, err := h.attempts.AbandonAsAdmin(t.Context(), provisioning); err != nil {
		t.Fatalf("abandon: %v", err)
	}
	requireError(t, h.do(http.MethodPost, "/api/attempts/"+provisioning+"/submit", ""), http.StatusConflict, "attempt_finished")
	requireError(t, h.do(http.MethodPost, "/api/attempts/NOPE/submit", ""), http.StatusNotFound, "not_found")
}
