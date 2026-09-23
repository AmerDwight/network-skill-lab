package api

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func dialTerminal(t *testing.T, h *harness, id, node, tab string) *websocket.Conn {
	t.Helper()
	return dial(t, h, "/ws/attempts/"+id+"/term/"+node+"/"+tab)
}

func sendText(t *testing.T, conn *websocket.Conn, text string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(text)); err != nil {
		t.Fatalf("write text: %v", err)
	}
}

func sendBinary(t *testing.T, conn *websocket.Conn, data []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageBinary, data); err != nil {
		t.Fatalf("write binary: %v", err)
	}
}

func TestTerminalRejectsBadRequests(t *testing.T) {
	h := newHarness(t)
	id := h.running()

	tests := []struct {
		name   string
		path   string
		status int
		code   string
	}{
		{"invalid tab", "/ws/attempts/" + id + "/term/web01/bad%20tab", http.StatusBadRequest, "bad_request"},
		{"unknown node", "/ws/attempts/" + id + "/term/nope/main", http.StatusNotFound, "unknown_node"},
		{"unknown attempt", "/ws/attempts/01NOPE/term/web01/main", http.StatusNotFound, "not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireError(t, h.do(http.MethodGet, tt.path, ""), tt.status, tt.code)
		})
	}
}

func TestTerminalRejectsFinishedAttempt(t *testing.T) {
	h := newHarness(t)
	id := h.running()
	h.do(http.MethodPost, "/api/attempts/"+id+"/abandon", "")

	requireError(t, h.do(http.MethodGet, "/ws/attempts/"+id+"/term/web01/main", ""), http.StatusConflict, "attempt_finished")
}

func TestTerminalRoundTrip(t *testing.T) {
	h := newHarness(t)
	id := h.running()

	conn := dialTerminal(t, h, id, "web01", "main")
	pty := h.runner.terminal(t, 0)

	waitUntil(t, "the default size to be applied", func() bool {
		cols, rows := pty.size()
		return cols == defaultCols && rows == defaultRows
	})

	sendText(t, conn, `{"type":"resize","cols":120,"rows":40}`)
	waitUntil(t, "the resize to be applied", func() bool {
		cols, rows := pty.size()
		return cols == 120 && rows == 40
	})

	sendBinary(t, conn, []byte("ip link\r"))
	waitUntil(t, "the input to reach the pty", func() bool {
		return strings.Contains(pty.received(), "ip link\r")
	})

	if err := pty.emit([]byte("web01$ ")); err != nil {
		t.Fatalf("emit output: %v", err)
	}
	kind, data := readFrame(t, conn)
	if kind != websocket.MessageBinary || string(data) != "web01$ " {
		t.Fatalf("output frame = %v %q", kind, data)
	}

	path := filepath.Join(h.dataDir, "recordings", id, "web01-main.cast")
	waitUntil(t, "the recording to be written", func() bool {
		_, err := os.Stat(path)
		return err == nil
	})
}

func TestTerminalSendsExitOnEOF(t *testing.T) {
	h := newHarness(t)
	id := h.running()

	conn := dialTerminal(t, h, id, "web01", "main")
	pty := h.runner.terminal(t, 0)
	pty.eof()

	message := readMessage(t, conn)
	if message["type"] != "exit" {
		t.Fatalf("message = %v, want an exit message", message)
	}

	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	defer cancel()
	if _, _, err := conn.Read(ctx); websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatalf("read after exit = %v, want a normal closure", err)
	}
	waitUntil(t, "the pty to be closed", pty.isClosed)
}

func TestTerminalReplacesPreviousConnection(t *testing.T) {
	h := newHarness(t)
	id := h.running()

	first := dialTerminal(t, h, id, "web01", "main")
	firstPTY := h.runner.terminal(t, 0)
	waitUntil(t, "the first terminal to be attached to its session", func() bool {
		cols, rows := firstPTY.size()
		return cols == defaultCols && rows == defaultRows
	})

	second := dialTerminal(t, h, id, "web01", "main")
	secondPTY := h.runner.terminal(t, 1)

	if !firstPTY.isClosed() {
		t.Fatal("the replaced terminal is still open")
	}

	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	defer cancel()
	if _, _, err := first.Read(ctx); err == nil {
		t.Fatal("the replaced connection is still readable")
	}

	if err := secondPTY.emit([]byte("alive")); err != nil {
		t.Fatalf("emit output: %v", err)
	}
	kind, data := readFrame(t, second)
	if kind != websocket.MessageBinary || string(data) != "alive" {
		t.Fatalf("output frame = %v %q", kind, data)
	}
	if secondPTY.isClosed() {
		t.Error("the replacing terminal was closed")
	}
}

func TestTerminalClosesATerminalOpenedAfterItsSessionWasReplaced(t *testing.T) {
	h := newHarness(t)
	id := h.running()
	release := h.runner.holdTerminals()

	first := dialTerminal(t, h, id, "web01", "main")
	firstPTY := h.runner.terminal(t, 0)

	dialTerminal(t, h, id, "web01", "main")
	secondPTY := h.runner.terminal(t, 1)

	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	defer cancel()
	if _, _, err := first.Read(ctx); err == nil {
		t.Fatal("the replaced connection is still readable")
	}

	release()
	waitUntil(t, "the terminal of the replaced session to be closed", firstPTY.isClosed)
	if secondPTY.isClosed() {
		t.Error("the replacing terminal was closed")
	}
}

func TestTerminalDisconnectsSlowClient(t *testing.T) {
	h := newHarness(t)
	id := h.running()

	dialTerminal(t, h, id, "web01", "main")
	pty := h.runner.terminal(t, 0)

	chunk := bytes.Repeat([]byte("x"), 32<<10)
	deadline := time.Now().Add(30 * time.Second)
	for !pty.isClosed() && time.Now().Before(deadline) {
		if err := pty.emit(chunk); err != nil {
			break
		}
	}
	waitUntil(t, "the slow client to be disconnected", pty.isClosed)
}
