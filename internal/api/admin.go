package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/AmerDwight/network-skill-lab/internal/attempt"
	"github.com/AmerDwight/network-skill-lab/internal/auth"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

func (s *server) listUsers(w http.ResponseWriter, r *http.Request) {
	summaries, err := s.store.Users.List(r.Context())
	if err != nil {
		s.fail(w, "", err)
		return
	}
	users := make([]userJSON, 0, len(summaries))
	for _, summary := range summaries {
		users = append(users, userOfSummary(summary))
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *server) createUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		s.fail(w, "", err)
		return
	}
	if body.Role == "" {
		body.Role = store.RoleUser
	}
	if body.Role != store.RoleUser && body.Role != store.RoleAdmin {
		s.fail(w, "", fmt.Errorf("%w: role must be %s or %s, got %q", errBadRequest, store.RoleUser, store.RoleAdmin, body.Role))
		return
	}
	if err := auth.ValidateUsername(body.Username); err != nil {
		s.fail(w, "", fmt.Errorf("%w: %s", errBadRequest, err))
		return
	}
	if err := auth.ValidatePassword(body.Password); err != nil {
		s.fail(w, "", fmt.Errorf("%w: %s", errBadRequest, err))
		return
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		s.fail(w, "", err)
		return
	}
	user := store.User{ID: store.NewID(), Username: body.Username, PasswordHash: hash, Role: body.Role}
	if err := s.store.Users.Create(r.Context(), user); err != nil {
		if errors.Is(err, store.ErrUsernameTaken) {
			writeError(w, http.StatusConflict, "username_taken", fmt.Sprintf("username %s is already taken", body.Username))
			return
		}
		s.fail(w, "", err)
		return
	}

	created, err := s.store.Users.SummaryByID(r.Context(), user.ID)
	if err != nil {
		s.fail(w, "", err)
		return
	}
	writeJSON(w, http.StatusCreated, userOfSummary(created))
}

func (s *server) patchUser(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "id")
	if id == userOf(r).ID {
		writeError(w, http.StatusBadRequest, "cannot_modify_self", "an administrator cannot change their own account here")
		return
	}

	var body struct {
		Role     *string `json:"role"`
		Disabled *bool   `json:"disabled"`
		Password *string `json:"password"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		s.fail(w, "", err)
		return
	}
	if _, err := s.store.Users.SummaryByID(r.Context(), id); err != nil {
		s.fail(w, "", err)
		return
	}

	if body.Role != nil {
		if *body.Role != store.RoleUser && *body.Role != store.RoleAdmin {
			s.fail(w, "", fmt.Errorf("%w: role must be %s or %s, got %q", errBadRequest, store.RoleUser, store.RoleAdmin, *body.Role))
			return
		}
		if err := s.store.Users.SetRole(r.Context(), id, *body.Role); err != nil {
			s.fail(w, "", err)
			return
		}
	}
	if body.Password != nil {
		if err := auth.ValidatePassword(*body.Password); err != nil {
			s.fail(w, "", fmt.Errorf("%w: %s", errBadRequest, err))
			return
		}
		hash, err := auth.HashPassword(*body.Password)
		if err != nil {
			s.fail(w, "", err)
			return
		}
		if err := s.store.Users.SetPasswordHash(r.Context(), id, hash); err != nil {
			s.fail(w, "", err)
			return
		}
	}
	if body.Disabled != nil {
		if err := s.store.Users.SetDisabled(r.Context(), id, *body.Disabled); err != nil {
			s.fail(w, "", err)
			return
		}
		if *body.Disabled {
			if err := s.store.Sessions.DeleteByUser(r.Context(), id); err != nil {
				s.fail(w, "", err)
				return
			}
		}
	}

	updated, err := s.store.Users.SummaryByID(r.Context(), id)
	if err != nil {
		s.fail(w, "", err)
		return
	}
	writeJSON(w, http.StatusOK, userOfSummary(updated))
}

func (s *server) adminAttempts(w http.ResponseWriter, r *http.Request) {
	views, err := s.attempts.ListActive(r.Context())
	if err != nil {
		s.fail(w, "", err)
		return
	}
	names, err := s.usernames(r)
	if err != nil {
		s.fail(w, "", err)
		return
	}
	lang := langOf(r)
	attempts := make([]adminAttemptJSON, 0, len(views))
	for _, view := range views {
		attempts = append(attempts, adminAttemptJSON{
			ID:        view.Id,
			Lab:       s.labSummaryOf(view.Lab, lang),
			Mode:      view.Mode,
			Status:    view.Status,
			ElapsedMS: view.ElapsedMS,
			CreatedAt: formatTime(view.CreatedAt),
			User:      attemptUserJSON{ID: view.UserID, Username: names[view.UserID]},
		})
	}
	writeJSON(w, http.StatusOK, attempts)
}

func (s *server) adminAbandon(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "id")
	view, err := s.attempts.AbandonAsAdmin(r.Context(), id)
	if err != nil {
		s.fail(w, id, err)
		return
	}
	writeJSON(w, http.StatusOK, s.attemptOf(view, langOf(r)))
}

func (s *server) adminDeleteAttempt(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "id")
	if err := s.attempts.Delete(r.Context(), id); err != nil {
		s.fail(w, id, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) adminStats(w http.ResponseWriter, r *http.Request) {
	active, err := s.store.Attempts.CountActive(r.Context())
	if err != nil {
		s.fail(w, "", err)
		return
	}
	bytes, err := s.store.Recordings.TotalBytes(r.Context())
	if err != nil {
		s.fail(w, "", err)
		return
	}
	total, err := s.store.Attempts.Count(r.Context())
	if err != nil {
		s.fail(w, "", err)
		return
	}
	writeJSON(w, http.StatusOK, statsJSON{
		SandboxesActive: active,
		SandboxesMax:    s.maxSandboxes,
		RecordingsBytes: bytes,
		Attempts:        total,
	})
}

func (s *server) usernames(r *http.Request) (map[string]string, error) {
	summaries, err := s.store.Users.List(r.Context())
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(summaries))
	for _, summary := range summaries {
		names[summary.ID] = summary.Username
	}
	return names, nil
}

func (s *server) failBusy(w http.ResponseWriter, err error) bool {
	var busy attempt.RunnerBusy
	if !errors.As(err, &busy) {
		return false
	}
	writeJSON(w, http.StatusTooManyRequests, runnerBusyJSON{
		Error:           errorBody{Code: "runner_busy", Message: err.Error()},
		SandboxesActive: busy.Active,
		SandboxesMax:    busy.Max,
	})
	return true
}
