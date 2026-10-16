package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/attempt"
	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/runnerserver"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

const defaultRunnerCapacity = 3

func (s *server) listRunners(w http.ResponseWriter, r *http.Request) {
	runners, err := s.runnerList(r.Context())
	if err != nil {
		s.fail(w, "", err)
		return
	}
	writeJSON(w, http.StatusOK, runners)
}

func (s *server) createRunner(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name         string   `json:"name"`
		Capabilities []string `json:"capabilities"`
		Capacity     *int     `json:"capacity"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		s.fail(w, "", err)
		return
	}
	if err := runnerserver.ValidateName(body.Name); err != nil {
		s.fail(w, "", fmt.Errorf("%w: %s", errBadRequest, err))
		return
	}
	if body.Name == runner.LocalName {
		s.fail(w, "", fmt.Errorf("%w: the name %s is reserved for the in-process runner", errBadRequest, runner.LocalName))
		return
	}
	for _, capability := range body.Capabilities {
		if capability != string(runner.EnvContainer) && capability != string(runner.EnvVM) {
			s.fail(w, "", fmt.Errorf("%w: capability must be %s or %s, got %q", errBadRequest, runner.EnvContainer, runner.EnvVM, capability))
			return
		}
	}
	capacity := defaultRunnerCapacity
	if body.Capacity != nil {
		capacity = *body.Capacity
	}
	if capacity < 1 {
		s.fail(w, "", fmt.Errorf("%w: capacity must be at least 1, got %d", errBadRequest, capacity))
		return
	}

	record, token, err := runnerserver.Register(r.Context(), s.store.Runners, body.Name, body.Capabilities, capacity)
	if errors.Is(err, store.ErrRunnerNameTaken) {
		writeError(w, http.StatusConflict, "runner_exists", fmt.Sprintf("runner %s already exists", body.Name))
		return
	}
	if err != nil {
		s.fail(w, "", err)
		return
	}
	s.log.Info("runner registered", "runner", record.Name, "by", userOf(r).Username)
	writeJSON(w, http.StatusCreated, createdRunnerJSON{ID: record.ID, Name: record.Name, Token: token})
}

func (s *server) revokeRunner(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "id")
	records, err := s.store.Runners.List(r.Context())
	if err != nil {
		s.fail(w, "", err)
		return
	}
	i := slices.IndexFunc(records, func(record store.Runner) bool { return record.ID == id })
	if i < 0 {
		writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("no runner with id %s", id))
		return
	}
	if err := s.store.Runners.Revoke(r.Context(), id); err != nil {
		s.fail(w, "", err)
		return
	}
	if s.hub != nil {
		s.hub.Disconnect(records[i].Name)
	}
	s.log.Info("runner revoked", "runner", records[i].Name, "by", userOf(r).Username)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) runnerSummary(w http.ResponseWriter, r *http.Request) {
	statuses, err := s.attempts.RunnerStatuses(r.Context())
	if err != nil {
		s.fail(w, "", err)
		return
	}
	summary := make([]runnerSummaryJSON, 0, len(statuses))
	for _, status := range statuses {
		summary = append(summary, runnerSummaryJSON{
			Name:         status.Name,
			Capabilities: envNames(status.Capabilities),
			Connected:    status.Connected,
		})
	}
	writeJSON(w, http.StatusOK, summary)
}

// runnerList merges the registered runners with what the hub saw of them since this
// process started; the in-process runner, which has no row, comes first with no id.
func (s *server) runnerList(ctx context.Context) ([]runnerJSON, error) {
	statuses, err := s.attempts.RunnerStatuses(ctx)
	if err != nil {
		return nil, err
	}
	records, err := s.store.Runners.List(ctx)
	if err != nil {
		return nil, err
	}
	live := map[string]runnerserver.RunnerInfo{}
	if s.hub != nil {
		for _, info := range s.hub.Runners() {
			live[info.Name] = info
		}
	}
	byName := make(map[string]attempt.RunnerStatus, len(statuses))
	for _, status := range statuses {
		byName[status.Name] = status
	}

	runners := make([]runnerJSON, 0, len(records)+1)
	if local, ok := byName[runner.LocalName]; ok {
		runners = append(runners, runnerJSON{
			Name:         local.Name,
			Capabilities: envNames(local.Capabilities),
			Capacity:     local.Capacity,
			Active:       local.Active,
			Connected:    true,
		})
	}
	for _, record := range records {
		item := runnerJSON{
			ID:           record.ID,
			Name:         record.Name,
			Capabilities: record.Capabilities,
			Capacity:     record.Capacity,
			Active:       byName[record.Name].Active,
			LastSeenAt:   formatTimePtr(record.LastSeenAt),
			Version:      record.Version,
			RevokedAt:    formatTimePtr(record.RevokedAt),
		}
		if info, ok := live[record.Name]; ok {
			item.Connected = info.Connected
			item.Capabilities = envNames(info.Capabilities)
			item.Capacity = info.Capacity
			item.Version = info.Version
			item.LastSeenAt = formatTimePtr(latest(record.LastSeenAt, info.LastSeen))
		}
		if item.Capabilities == nil {
			item.Capabilities = []string{}
		}
		runners = append(runners, item)
	}
	return runners, nil
}

func latest(stored *time.Time, seen time.Time) *time.Time {
	if stored != nil && stored.After(seen) {
		return stored
	}
	if seen.IsZero() {
		return stored
	}
	return &seen
}

func envNames(envs []runner.Env) []string {
	names := make([]string, 0, len(envs))
	for _, env := range envs {
		names = append(names, string(env))
	}
	return names
}

func environmentOf(lab content.Lab) string {
	if lab.Environment == "" {
		return string(runner.EnvContainer)
	}
	return lab.Environment
}
