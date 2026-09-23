package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/AmerDwight/network-skill-lab/internal/attempt"
	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/recorder"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/store"
	"github.com/go-chi/chi/v5"
)

const maxRequestBytes = 64 << 10

var errBadRequest = errors.New("bad request")

type Deps struct {
	Attempts *attempt.Service
	Labs     []content.Lab
	Store    *store.Store
	Runner   runner.Runner
	Recorder *recorder.Recorder
	Logger   *slog.Logger
}

type server struct {
	attempts  *attempt.Service
	labs      []content.Lab
	byID      map[string]content.Lab
	store     *store.Store
	runner    runner.Runner
	recorder  *recorder.Recorder
	log       *slog.Logger
	terminals *terminals
}

func New(deps Deps) http.Handler {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	byID := make(map[string]content.Lab, len(deps.Labs))
	for _, lab := range deps.Labs {
		byID[lab.Id] = lab
	}
	s := &server{
		attempts:  deps.Attempts,
		labs:      deps.Labs,
		byID:      byID,
		store:     deps.Store,
		runner:    deps.Runner,
		recorder:  deps.Recorder,
		log:       deps.Logger,
		terminals: newTerminals(),
	}

	r := chi.NewRouter()
	r.Get("/api/health", s.health)
	r.Get("/api/labs", s.listLabs)
	r.Get("/api/labs/{id}", s.getLab)
	r.Post("/api/attempts", s.createAttempt)
	r.Get("/api/attempts/current", s.currentAttempt)
	r.Get("/api/attempts/{id}", s.getAttempt)
	r.Post("/api/attempts/{id}/abandon", s.abandonAttempt)
	r.Get("/api/attempts/{id}/result", s.attemptResult)
	r.Get("/ws/attempts/{id}/events", s.events)
	r.Get("/ws/attempts/{id}/term/{node}/{tab}", s.terminal)
	r.NotFound(notFound)
	r.MethodNotAllowed(methodNotAllowed)
	return r
}

func langOf(r *http.Request) string {
	switch r.URL.Query().Get("lang") {
	case "en":
		return "en"
	default:
		return "zh"
	}
}

func pathParam(r *http.Request, name string) string {
	value := chi.URLParam(r, name)
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return value
	}
	return decoded
}

func decodeBody(w http.ResponseWriter, r *http.Request, body any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	if err := json.NewDecoder(r.Body).Decode(body); err != nil {
		return fmt.Errorf("%w: decode body: %s", errBadRequest, err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	body := struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{}
	body.Error.Code = code
	body.Error.Message = message
	writeJSON(w, status, body)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("no route for %s %s", r.Method, r.URL.Path))
}

func methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", fmt.Sprintf("%s is not allowed on %s", r.Method, r.URL.Path))
}

func (s *server) fail(w http.ResponseWriter, attemptID string, err error) {
	status, code := classify(err)
	if status == http.StatusInternalServerError {
		s.log.Error("request failed", "attempt", attemptID, "error", err)
	}
	writeError(w, status, code, err.Error())
}

func classify(err error) (int, string) {
	switch {
	case errors.Is(err, attempt.ErrActiveAttempt):
		return http.StatusConflict, "attempt_active"
	case errors.Is(err, attempt.ErrNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, attempt.ErrUnknownLab):
		return http.StatusNotFound, "unknown_lab"
	case errors.Is(err, attempt.ErrModeNotAllowed):
		return http.StatusBadRequest, "mode_not_allowed"
	case errors.Is(err, attempt.ErrTerminal):
		return http.StatusConflict, "attempt_finished"
	case errors.Is(err, attempt.ErrNotTerminal):
		return http.StatusConflict, "attempt_running"
	case errors.Is(err, errBadRequest):
		return http.StatusBadRequest, "bad_request"
	default:
		return http.StatusInternalServerError, "internal"
	}
}

func terminalStatus(status string) bool {
	switch status {
	case store.StatusProvisioning, store.StatusRunning:
		return false
	default:
		return true
	}
}
