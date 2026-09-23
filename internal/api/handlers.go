package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/AmerDwight/network-skill-lab/internal/attempt"
)

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, healthOf(s.runner.Health(r.Context())))
}

func (s *server) listLabs(w http.ResponseWriter, r *http.Request) {
	lang := langOf(r)
	labs := make([]labSummary, 0, len(s.labs))
	for _, lab := range s.labs {
		labs = append(labs, labSummaryOf(lab, lang))
	}
	writeJSON(w, http.StatusOK, labs)
}

func (s *server) getLab(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "id")
	lab, ok := s.byID[id]
	if !ok {
		s.fail(w, "", fmt.Errorf("%w: %s", attempt.ErrUnknownLab, id))
		return
	}
	writeJSON(w, http.StatusOK, labDetailOf(lab, langOf(r)))
}

func (s *server) createAttempt(w http.ResponseWriter, r *http.Request) {
	var body struct {
		LabID string `json:"lab_id"`
		Mode  string `json:"mode"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		s.fail(w, "", err)
		return
	}

	user, err := s.store.Users.Local(r.Context())
	if err != nil {
		s.fail(w, "", err)
		return
	}
	view, err := s.attempts.Start(r.Context(), user.ID, body.LabID, body.Mode)
	if err != nil {
		s.fail(w, "", err)
		return
	}
	writeJSON(w, http.StatusCreated, attemptOf(view, langOf(r)))
}

func (s *server) currentAttempt(w http.ResponseWriter, r *http.Request) {
	user, err := s.store.Users.Local(r.Context())
	if err != nil {
		s.fail(w, "", err)
		return
	}
	view, ok, err := s.attempts.Current(r.Context(), user.ID)
	if err != nil {
		s.fail(w, "", err)
		return
	}
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, attemptOf(view, langOf(r)))
}

func (s *server) getAttempt(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "id")
	view, err := s.attempts.Get(r.Context(), id)
	if err != nil {
		s.fail(w, id, err)
		return
	}
	writeJSON(w, http.StatusOK, attemptOf(view, langOf(r)))
}

func (s *server) abandonAttempt(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "id")
	view, err := s.attempts.Abandon(r.Context(), id)
	if err != nil {
		s.fail(w, id, err)
		return
	}
	writeJSON(w, http.StatusOK, attemptOf(view, langOf(r)))
}

func (s *server) attemptResult(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "id")
	lang := langOf(r)

	view, err := s.attempts.Get(r.Context(), id)
	if err != nil {
		s.fail(w, id, err)
		return
	}
	if !terminalStatus(view.Status) {
		s.fail(w, id, fmt.Errorf("%w: %s is %s", attempt.ErrNotTerminal, id, view.Status))
		return
	}

	count, err := s.store.CommandLog.CountByAttempt(r.Context(), id)
	if err != nil {
		s.fail(w, id, err)
		return
	}
	path := filepath.Join(view.Lab.Dir, view.Lab.Solution.Get(lang))
	solution, err := os.ReadFile(path)
	if err != nil {
		s.fail(w, id, fmt.Errorf("read solution %s: %w", path, err))
		return
	}
	writeJSON(w, http.StatusOK, resultOf(view, lang, count, string(solution)))
}
