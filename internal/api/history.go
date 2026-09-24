package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/attempt"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

const (
	defaultHistoryLimit  = 50
	maxHistoryLimit      = 200
	defaultCommandLimit  = 500
	maxCommandLimit      = 2000
	castContentType      = "text/plain; charset=utf-8"
	historyUserQueryName = "user_id"
)

func (s *server) history(w http.ResponseWriter, r *http.Request) {
	caller := userOf(r)
	owner := caller
	if wanted := r.URL.Query().Get(historyUserQueryName); wanted != "" && wanted != caller.ID {
		if caller.Role != store.RoleAdmin {
			writeError(w, http.StatusForbidden, "forbidden", "only administrators can read another user's history")
			return
		}
		found, err := s.store.Users.ByID(r.Context(), wanted)
		if err != nil {
			s.fail(w, "", err)
			return
		}
		owner = found
	}

	limit, err := intQuery(r, "limit", defaultHistoryLimit, maxHistoryLimit)
	if err != nil {
		s.fail(w, "", err)
		return
	}
	var before time.Time
	if raw := r.URL.Query().Get("before"); raw != "" {
		before, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			s.fail(w, "", fmt.Errorf("%w: before must be an RFC 3339 timestamp: %s", errBadRequest, err))
			return
		}
	}

	items, err := s.attempts.ListHistory(r.Context(), owner.ID, before, limit)
	if err != nil {
		s.fail(w, "", err)
		return
	}
	lang := langOf(r)
	history := make([]historyJSON, 0, len(items))
	for _, item := range items {
		history = append(history, historyJSON{
			ID:           item.Id,
			Lab:          s.labSummaryOf(item.Lab, lang),
			Mode:         item.Mode,
			Status:       item.Status,
			ElapsedMS:    item.ElapsedMS,
			SubmitCount:  item.SubmitCount,
			CommandCount: item.CommandCount,
			CreatedAt:    formatTime(item.CreatedAt),
			EndedAt:      formatTimePtr(item.EndedAt),
			User:         attemptUserJSON{ID: owner.ID, Username: owner.Username},
		})
	}
	writeJSON(w, http.StatusOK, history)
}

func (s *server) attemptCommands(w http.ResponseWriter, r *http.Request) {
	view, err := s.readableAttempt(r)
	if err != nil {
		s.fail(w, "", err)
		return
	}
	limit, err := intQuery(r, "limit", defaultCommandLimit, maxCommandLimit)
	if err != nil {
		s.fail(w, "", err)
		return
	}
	var after int64
	if raw := r.URL.Query().Get("after"); raw != "" {
		after, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			s.fail(w, "", fmt.Errorf("%w: after must be a command id: %s", errBadRequest, err))
			return
		}
	}

	entries, err := s.store.CommandLog.ListByAttemptAfter(r.Context(), view.Id, after, limit)
	if err != nil {
		s.fail(w, view.Id, err)
		return
	}
	commands := make([]commandJSON, 0, len(entries))
	for _, entry := range entries {
		commands = append(commands, commandJSON{
			ID:       strconv.FormatInt(entry.ID, 10),
			Node:     entry.Node,
			TS:       formatTime(entry.TS),
			User:     entry.User,
			CWD:      entry.CWD,
			Command:  entry.Command,
			ExitCode: entry.ExitCode,
		})
	}
	writeJSON(w, http.StatusOK, commands)
}

func (s *server) attemptRecordings(w http.ResponseWriter, r *http.Request) {
	view, err := s.readableAttempt(r)
	if err != nil {
		s.fail(w, "", err)
		return
	}
	found, err := s.store.Recordings.ListByAttempt(r.Context(), view.Id)
	if err != nil {
		s.fail(w, view.Id, err)
		return
	}
	recordings := make([]recordingJSON, 0, len(found))
	for _, rec := range found {
		var size int64
		if rec.Bytes != nil {
			size = *rec.Bytes
		}
		recordings = append(recordings, recordingJSON{
			ID:        rec.ID,
			Node:      rec.Node,
			Tab:       rec.TabID,
			StartedAt: formatTime(rec.StartedAt),
			EndedAt:   formatTimePtr(rec.EndedAt),
			Bytes:     size,
		})
	}
	writeJSON(w, http.StatusOK, recordings)
}

func (s *server) attemptCast(w http.ResponseWriter, r *http.Request) {
	view, err := s.readableAttempt(r)
	if err != nil {
		s.fail(w, "", err)
		return
	}
	recordingID := pathParam(r, "rid")
	found, err := s.store.Recordings.ListByAttempt(r.Context(), view.Id)
	if err != nil {
		s.fail(w, view.Id, err)
		return
	}
	path := ""
	for _, rec := range found {
		if rec.ID == recordingID {
			path = rec.Path
			break
		}
	}
	if path == "" {
		writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("attempt %s has no recording %s", view.Id, recordingID))
		return
	}

	file, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("recording %s is no longer on disk", recordingID))
		return
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		s.fail(w, view.Id, err)
		return
	}
	w.Header().Set("Content-Type", castContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	if _, err := io.Copy(w, file); err != nil {
		s.log.Warn("stream recording", "attempt", view.Id, "recording", recordingID, "error", err)
	}
}

func (s *server) ownedAttempt(r *http.Request) (attempt.View, error) {
	return s.attempts.ForUser(r.Context(), userOf(r).ID, pathParam(r, "id"))
}

func (s *server) readableAttempt(r *http.Request) (attempt.View, error) {
	user := userOf(r)
	id := pathParam(r, "id")
	if user.Role != store.RoleAdmin {
		return s.attempts.ForUser(r.Context(), user.ID, id)
	}
	view, err := s.attempts.Get(r.Context(), id)
	if err != nil {
		return attempt.View{}, err
	}
	if view.UserID != user.ID && !terminalStatus(view.Status) {
		return attempt.View{}, fmt.Errorf("%w: %s", attempt.ErrNotFound, id)
	}
	return view, nil
}

func intQuery(r *http.Request, name string, fallback, max int) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%w: %s must be a number: %s", errBadRequest, name, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%w: %s must be positive, got %d", errBadRequest, name, value)
	}
	return min(value, max), nil
}
