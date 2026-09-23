package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sync"

	"github.com/AmerDwight/network-skill-lab/internal/attempt"
	"github.com/AmerDwight/network-skill-lab/internal/recorder"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/store"
	"github.com/coder/websocket"
)

const (
	defaultCols   = 80
	defaultRows   = 24
	maxDimension  = 10000
	ptyBufferSize = 32 << 10
)

var tabPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

type resizeMessage struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

type exitMessage struct {
	Type string `json:"type"`
}

type terminalKey struct {
	attempt string
	node    string
	tab     string
}

type terminalSession struct {
	socket *socket
	cancel context.CancelFunc

	once   sync.Once
	mu     sync.Mutex
	pty    runner.PTY
	cast   *recorder.Cast
	closed bool
}

func (t *terminalSession) attach(pty runner.PTY, cast *recorder.Cast) bool {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		_ = pty.Close()
		_ = cast.Close()
		return false
	}
	t.pty, t.cast = pty, cast
	t.mu.Unlock()
	return true
}

func (t *terminalSession) close() {
	t.once.Do(func() {
		t.mu.Lock()
		pty, cast := t.pty, t.cast
		t.closed = true
		t.mu.Unlock()
		t.socket.out.finish()
		if pty != nil {
			_ = pty.Close()
		}
		if cast != nil {
			_ = cast.Close()
		}
		t.cancel()
		// CloseNow waits for an in-flight graceful close of the same connection,
		// which must not hold up the request that is replacing this session.
		go func() { _ = t.socket.conn.CloseNow() }()
	})
}

type terminals struct {
	mu   sync.Mutex
	open map[terminalKey]*terminalSession
}

func newTerminals() *terminals {
	return &terminals{open: map[terminalKey]*terminalSession{}}
}

func (t *terminals) put(key terminalKey, session *terminalSession) {
	t.mu.Lock()
	previous := t.open[key]
	t.open[key] = session
	t.mu.Unlock()
	if previous != nil {
		previous.close()
	}
}

func (t *terminals) remove(key terminalKey, session *terminalSession) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.open[key] == session {
		delete(t.open, key)
	}
}

func (s *server) terminal(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "id")
	node := pathParam(r, "node")
	tab := pathParam(r, "tab")

	if !tabPattern.MatchString(tab) {
		writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("invalid tab %q", tab))
		return
	}
	view, err := s.attempts.Get(r.Context(), id)
	if err != nil {
		s.fail(w, id, err)
		return
	}
	if view.Status != store.StatusRunning {
		code := "attempt_provisioning"
		if terminalStatus(view.Status) {
			code = "attempt_finished"
		}
		writeError(w, http.StatusConflict, code, fmt.Sprintf("attempt %s is %s", id, view.Status))
		return
	}
	if !hasNode(view.Nodes, node) {
		writeError(w, http.StatusNotFound, "unknown_node", fmt.Sprintf("attempt %s has no node %s", id, node))
		return
	}

	conn, err := accept(w, r)
	if err != nil {
		s.log.Warn("accept terminal socket", "attempt", id, "node", node, "tab", tab, "error", err)
		return
	}

	s.attempts.Connected(id)
	defer s.attempts.Disconnected(id)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	sock := newSocket(conn, s.log, id)
	sock.start(ctx)
	session := &terminalSession{socket: sock, cancel: cancel}
	defer session.close()

	key := terminalKey{attempt: id, node: node, tab: tab}
	s.terminals.put(key, session)
	defer s.terminals.remove(key, session)

	pty, err := s.runner.OpenTerminal(ctx, runner.SandboxID(view.SandboxID), node)
	if err != nil {
		s.log.Error("open terminal", "attempt", id, "node", node, "error", err)
		sock.sendJSON(errorMessage{Type: "error", Message: "cannot open a terminal on " + node})
		sock.shutdown("terminal unavailable")
		return
	}
	cast, err := s.recorder.OpenRecording(ctx, id, node, tab, defaultCols, defaultRows)
	if err != nil {
		s.log.Error("open recording", "attempt", id, "node", node, "tab", tab, "error", err)
		_ = pty.Close()
		sock.sendJSON(errorMessage{Type: "error", Message: "cannot record this terminal"})
		sock.shutdown("recording unavailable")
		return
	}
	if !session.attach(pty, cast) {
		return
	}

	if err := pty.Resize(defaultCols, defaultRows); err != nil {
		s.log.Warn("resize terminal", "attempt", id, "node", node, "error", err)
	}

	go s.pump(session, pty, cast)
	s.readClient(ctx, conn, pty, cast, id, node)
}

func (s *server) pump(session *terminalSession, pty runner.PTY, cast *recorder.Cast) {
	defer session.close()

	buf := make([]byte, ptyBufferSize)
	for {
		n, err := pty.Read(buf)
		if n > 0 {
			data := bytes.Clone(buf[:n])
			if err := cast.Output(data); err != nil {
				s.log.Warn("record terminal output", "attempt", session.socket.attemptID, "error", err)
			}
			if !session.socket.sendBinary(data) {
				return
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				session.socket.sendJSON(exitMessage{Type: "exit"})
				session.socket.shutdown("shell exited")
				return
			}
			s.log.Warn("read terminal", "attempt", session.socket.attemptID, "error", err)
			session.socket.sendJSON(errorMessage{Type: "error", Message: "terminal read failed"})
			session.socket.shutdown("terminal failed")
			return
		}
	}
}

func (s *server) readClient(ctx context.Context, conn *websocket.Conn, pty runner.PTY, cast *recorder.Cast, id, node string) {
	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		switch kind {
		case websocket.MessageBinary:
			if _, err := pty.Write(data); err != nil {
				s.log.Warn("write to terminal", "attempt", id, "node", node, "error", err)
				return
			}
			if err := cast.Input(data); err != nil {
				s.log.Warn("record terminal input", "attempt", id, "error", err)
			}
		case websocket.MessageText:
			var message resizeMessage
			if err := json.Unmarshal(data, &message); err != nil || message.Type != "resize" {
				continue
			}
			if !validDimension(message.Cols) || !validDimension(message.Rows) {
				continue
			}
			if err := pty.Resize(uint16(message.Cols), uint16(message.Rows)); err != nil {
				s.log.Warn("resize terminal", "attempt", id, "node", node, "error", err)
				continue
			}
			if err := cast.Resize(message.Cols, message.Rows); err != nil {
				s.log.Warn("record terminal resize", "attempt", id, "error", err)
			}
		}
	}
}

func validDimension(value int) bool {
	return value > 0 && value <= maxDimension
}

func hasNode(nodes []attempt.Node, name string) bool {
	for _, node := range nodes {
		if node.Name == name {
			return true
		}
	}
	return false
}
