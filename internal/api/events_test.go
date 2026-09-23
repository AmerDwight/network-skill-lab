package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/attempt"
	"github.com/AmerDwight/network-skill-lab/internal/store"
	"github.com/coder/websocket"
)

const idleTimeout = 15 * time.Minute

func dial(t *testing.T, h *harness, path string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, h.socketURL(path), nil)
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

func readFrame(t *testing.T, conn *websocket.Conn) (websocket.MessageType, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	defer cancel()
	kind, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read message: %v", err)
	}
	return kind, data
}

func readMessage(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	kind, data := readFrame(t, conn)
	if kind != websocket.MessageText {
		t.Fatalf("message kind = %v, want text", kind)
	}
	var message map[string]any
	if err := json.Unmarshal(data, &message); err != nil {
		t.Fatalf("decode message %q: %v", data, err)
	}
	return message
}

func waitForMessage(t *testing.T, conn *websocket.Conn, kind string) map[string]any {
	t.Helper()
	for range 32 {
		message := readMessage(t, conn)
		if message["type"] == kind {
			return message
		}
	}
	t.Fatalf("no %s message arrived", kind)
	return nil
}

func TestEventsSendsInitialStatus(t *testing.T) {
	h := newHarness(t)
	id := h.running()

	conn := dial(t, h, "/ws/attempts/"+id+"/events")
	message := readMessage(t, conn)
	requireKeys(t, message, "type", "status", "error_message", "elapsed_ms", "server_time")
	if message["type"] != "status" || message["status"] != store.StatusRunning {
		t.Errorf("first message = %v, want a running status", message)
	}
}

func TestEventsRelaysProvisioningAndStatus(t *testing.T) {
	h := newHarness(t)
	release := h.runner.hold()
	id := h.start()

	conn := dial(t, h, "/ws/attempts/"+id+"/events")
	if first := readMessage(t, conn); first["status"] != store.StatusProvisioning {
		t.Fatalf("first message = %v, want a provisioning status", first)
	}
	release()

	step := waitForMessage(t, conn, "provisioning")
	requireKeys(t, step, "type", "step")
	if step["step"] != "networks" {
		t.Errorf("step = %v, want networks", step["step"])
	}

	status := waitForMessage(t, conn, "status")
	if status["status"] != store.StatusRunning {
		t.Errorf("status = %v, want running", status["status"])
	}
}

func TestEventsRelaysCheckpoint(t *testing.T) {
	h := newHarness(t)
	id := h.running()

	conn := dial(t, h, "/ws/attempts/"+id+"/events")
	readMessage(t, conn)

	passed := time.Now().UTC()
	h.attempts.PublishCheckpoint(id, attempt.CheckpointEvent{
		Id:            "link-up",
		Status:        store.CheckpointPass,
		FirstPassedAt: &passed,
	})

	message := waitForMessage(t, conn, "checkpoint")
	requireKeys(t, message, "type", "id", "status", "first_passed_at")
	if message["id"] != "link-up" || message["status"] != store.CheckpointPass {
		t.Errorf("checkpoint = %v", message)
	}
	if message["first_passed_at"] != formatTime(passed) {
		t.Errorf("first_passed_at = %v, want %q", message["first_passed_at"], formatTime(passed))
	}
}

func TestEventsClosesOnTerminalStatus(t *testing.T) {
	h := newHarness(t)
	id := h.running()

	conn := dial(t, h, "/ws/attempts/"+id+"/events")
	readMessage(t, conn)

	h.do(http.MethodPost, "/api/attempts/"+id+"/abandon", "")

	status := waitForMessage(t, conn, "status")
	if status["status"] != store.StatusAbandoned {
		t.Fatalf("status = %v, want abandoned", status["status"])
	}

	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	defer cancel()
	_, _, err := conn.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatalf("read after the terminal status = %v, want a normal closure", err)
	}
}

func TestEventsRejectsUnknownAttempt(t *testing.T) {
	h := newHarness(t)
	requireError(t, h.do(http.MethodGet, "/ws/attempts/01NOPE/events", ""), http.StatusNotFound, "not_found")
}

func TestEventsConnectionCountsAsPresence(t *testing.T) {
	h := newHarness(t)
	id := h.running()

	conn := dial(t, h, "/ws/attempts/"+id+"/events")
	readMessage(t, conn)

	h.clock.fire(t, idleTimeout)
	time.Sleep(50 * time.Millisecond)
	if view, err := h.attempts.Get(t.Context(), id); err != nil || view.Status != store.StatusRunning {
		t.Fatalf("attempt = %v (%v), want it still running while a client is connected", view.Status, err)
	}

	_ = conn.Close(websocket.StatusNormalClosure, "")
	h.clock.fire(t, idleTimeout)

	waitUntil(t, "the attempt to expire once the client is gone", func() bool {
		view, err := h.attempts.Get(t.Context(), id)
		return err == nil && view.Status == store.StatusExpired
	})
}
