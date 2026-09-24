package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/AmerDwight/network-skill-lab/internal/attempt"
	"github.com/AmerDwight/network-skill-lab/internal/auth"
	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/recorder"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/store"
	"github.com/go-chi/chi/v5"
)

const (
	maxRequestBytes     = 64 << 10
	defaultMaxSandboxes = 3
)

var errBadRequest = errors.New("bad request")

type Deps struct {
	Attempts     *attempt.Service
	Content      *content.Content
	Store        *store.Store
	Runner       runner.Runner
	Recorder     *recorder.Recorder
	Auth         *auth.Service
	MaxSandboxes int
	Logger       *slog.Logger
}

type server struct {
	attempts     *attempt.Service
	content      *content.Content
	byID         map[string]content.Lab
	docs         map[string]content.Doc
	store        *store.Store
	runner       runner.Runner
	recorder     *recorder.Recorder
	auth         *auth.Service
	maxSandboxes int
	log          *slog.Logger
	terminals    *terminals
}

func New(deps Deps) http.Handler {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Content == nil {
		deps.Content = &content.Content{}
	}
	byID := make(map[string]content.Lab, len(deps.Content.Labs))
	for _, lab := range deps.Content.Labs {
		byID[lab.Id] = lab
	}
	docs := make(map[string]content.Doc, len(deps.Content.Docs))
	for _, doc := range deps.Content.Docs {
		docs[doc.ID] = doc
	}
	if deps.MaxSandboxes <= 0 {
		deps.MaxSandboxes = defaultMaxSandboxes
	}
	s := &server{
		attempts:     deps.Attempts,
		content:      deps.Content,
		byID:         byID,
		docs:         docs,
		store:        deps.Store,
		runner:       deps.Runner,
		recorder:     deps.Recorder,
		auth:         deps.Auth,
		maxSandboxes: deps.MaxSandboxes,
		log:          deps.Logger,
		terminals:    newTerminals(),
	}

	r := chi.NewRouter()
	r.Use(jsonWrites)
	r.Get("/api/health", s.health)
	r.Post("/api/auth/login", s.login)

	r.Group(func(r chi.Router) {
		r.Use(s.authenticate)
		r.Post("/api/auth/logout", s.logout)
		r.Get("/api/auth/me", s.me)
		r.Patch("/api/auth/me", s.updateMe)
		r.Get("/api/topics", s.listTopics)
		r.Get("/api/labs", s.listLabs)
		r.Get("/api/labs/{id}", s.getLab)
		r.Get("/api/docs", s.listDocs)
		r.Get("/api/docs/*", s.getDoc)
		r.Get("/api/tracks", s.listTracks)
		r.Get("/api/tracks/{id}", s.getTrack)
		r.Post("/api/progress", s.markProgress)
		r.Get("/api/history", s.history)
		r.Post("/api/attempts", s.createAttempt)
		r.Get("/api/attempts/current", s.currentAttempt)
		r.Get("/api/attempts/{id}", s.getAttempt)
		r.Post("/api/attempts/{id}/abandon", s.abandonAttempt)
		r.Post("/api/attempts/{id}/submit", s.submitAttempt)
		r.Get("/api/attempts/{id}/result", s.attemptResult)
		r.Get("/api/attempts/{id}/commands", s.attemptCommands)
		r.Get("/api/attempts/{id}/recordings", s.attemptRecordings)
		r.Get("/api/attempts/{id}/recordings/{rid}/cast", s.attemptCast)
		r.Get("/ws/attempts/{id}/events", s.events)
		r.Get("/ws/attempts/{id}/term/{node}/{tab}", s.terminal)

		r.Group(func(r chi.Router) {
			r.Use(requireAdmin)
			r.Get("/api/admin/users", s.listUsers)
			r.Post("/api/admin/users", s.createUser)
			r.Patch("/api/admin/users/{id}", s.patchUser)
			r.Get("/api/admin/attempts", s.adminAttempts)
			r.Post("/api/admin/attempts/{id}/abandon", s.adminAbandon)
			r.Delete("/api/admin/attempts/{id}", s.adminDeleteAttempt)
			r.Get("/api/admin/stats", s.adminStats)
		})
	})

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
	writeJSON(w, status, struct {
		Error errorBody `json:"error"`
	}{Error: errorBody{Code: code, Message: message}})
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
		writeError(w, status, code, "internal error")
		return
	}
	writeError(w, status, code, err.Error())
}

func classify(err error) (int, string) {
	switch {
	case errors.Is(err, attempt.ErrActiveAttempt):
		return http.StatusConflict, "attempt_active"
	case errors.Is(err, attempt.ErrNotFound), errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, attempt.ErrUnknownLab):
		return http.StatusNotFound, "unknown_lab"
	case errors.Is(err, attempt.ErrUnknownDoc):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, attempt.ErrModeNotAllowed):
		return http.StatusBadRequest, "mode_not_allowed"
	case errors.Is(err, attempt.ErrModeNotReal):
		return http.StatusBadRequest, "mode_not_real"
	case errors.Is(err, attempt.ErrProvisioning):
		return http.StatusConflict, "attempt_provisioning"
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
