package api

import (
	"net/http"

	"github.com/AmerDwight/network-skill-lab/internal/attempt"
)

type statusMessage struct {
	Type         string `json:"type"`
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message"`
	ElapsedMS    int64  `json:"elapsed_ms"`
	ServerTime   string `json:"server_time"`
}

type provisioningMessage struct {
	Type    string `json:"type"`
	Step    string `json:"step"`
	Attempt int    `json:"attempt,omitempty"`
}

type checkpointMessage struct {
	Type          string  `json:"type"`
	ID            string  `json:"id"`
	Status        string  `json:"status"`
	FirstPassedAt *string `json:"first_passed_at"`
}

type tickMessage struct {
	Type       string `json:"type"`
	ElapsedMS  int64  `json:"elapsed_ms"`
	ServerTime string `json:"server_time"`
}

type errorMessage struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (s *server) events(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "id")

	events, unsubscribe := s.attempts.Subscribe(id)
	defer unsubscribe()

	view, err := s.attempts.Get(r.Context(), id)
	if err != nil {
		s.fail(w, id, err)
		return
	}

	conn, err := accept(w, r)
	if err != nil {
		s.log.Warn("accept events socket", "attempt", id, "error", err)
		return
	}
	defer func() { _ = conn.CloseNow() }()

	s.attempts.Connected(id)
	defer s.attempts.Disconnected(id)

	ctx := conn.CloseRead(r.Context())
	sock := newSocket(conn, s.log, id)
	sock.start(ctx)

	status := statusMessage{
		Type:         attempt.EventStatus,
		Status:       view.Status,
		ErrorMessage: view.ErrorMessage,
		ElapsedMS:    view.ElapsedMS,
		ServerTime:   formatTime(view.ServerTime),
	}
	if !sock.sendJSON(status) {
		return
	}
	if terminalStatus(view.Status) {
		sock.shutdown(view.Status)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-sock.closed():
			return
		case ev, ok := <-events:
			if !ok {
				sock.shutdown("server shutting down")
				return
			}
			message, ok := eventMessage(ev)
			if !ok {
				continue
			}
			if !sock.sendJSON(message) {
				return
			}
			if ev.Type == attempt.EventStatus && terminalStatus(ev.Status) {
				sock.shutdown(ev.Status)
				return
			}
		}
	}
}

func eventMessage(ev attempt.Event) (any, bool) {
	switch ev.Type {
	case attempt.EventStatus:
		return statusMessage{
			Type:         attempt.EventStatus,
			Status:       ev.Status,
			ErrorMessage: ev.ErrorMessage,
			ElapsedMS:    ev.ElapsedMS,
			ServerTime:   formatTime(ev.ServerTime),
		}, true
	case attempt.EventProvisioning:
		return provisioningMessage{Type: attempt.EventProvisioning, Step: ev.Step, Attempt: ev.Attempt}, true
	case attempt.EventCheckpoint:
		if ev.Checkpoint == nil {
			return nil, false
		}
		return checkpointMessage{
			Type:          attempt.EventCheckpoint,
			ID:            ev.Checkpoint.Id,
			Status:        ev.Checkpoint.Status,
			FirstPassedAt: formatTimePtr(ev.Checkpoint.FirstPassedAt),
		}, true
	case attempt.EventTick:
		return tickMessage{
			Type:       attempt.EventTick,
			ElapsedMS:  ev.ElapsedMS,
			ServerTime: formatTime(ev.ServerTime),
		}, true
	case attempt.EventError:
		return errorMessage{Type: attempt.EventError, Message: ev.ErrorMessage}, true
	default:
		return nil, false
	}
}
