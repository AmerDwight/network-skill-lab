package attempt

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/store"
)

func (h *harness) storedAttempt(t *testing.T, status string, endedAgo time.Duration, cast string) string {
	t.Helper()
	now := h.clock.Now()
	att := store.Attempt{
		ID:         store.NewID(),
		UserID:     h.user,
		LabID:      h.lab.Id,
		LabVersion: 1,
		Mode:       "guided",
		ParamsJSON: "{}",
		Status:     status,
		CreatedAt:  now.Add(-endedAgo - time.Hour),
	}
	if status != store.StatusProvisioning && status != store.StatusRunning {
		ended := now.Add(-endedAgo)
		att.EndedAt = &ended
	}
	if err := h.store.Attempts.Create(t.Context(), att); err != nil {
		t.Fatalf("create attempt: %v", err)
	}
	if cast != "" {
		dir := filepath.Join(h.dataDir, recordingsDir, att.ID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create recordings dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "web01-main.cast"), []byte(cast), 0o644); err != nil {
			t.Fatalf("write cast: %v", err)
		}
	}
	return att.ID
}

func (h *harness) exists(t *testing.T, id string) bool {
	t.Helper()
	_, err := h.store.Attempts.Get(t.Context(), id)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get attempt %s: %v", id, err)
	}
	return err == nil
}

func TestPruneDryRunReportsWithoutDeleting(t *testing.T) {
	h := newHarness(t, &fakeRunner{})
	old := h.storedAttempt(t, store.StatusPassed, 40*24*time.Hour, "0123456789")
	recent := h.storedAttempt(t, store.StatusPassed, time.Hour, "abc")

	report, err := h.Prune(t.Context(), 30*24*time.Hour, true)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if report.Attempts != 1 || report.Bytes != 10 || !slices.Equal(report.IDs, []string{old}) {
		t.Errorf("report = %+v, want one attempt of 10 bytes (%s)", report, old)
	}
	for _, id := range []string{old, recent} {
		if !h.exists(t, id) {
			t.Errorf("attempt %s was deleted by a dry run", id)
		}
	}
	if _, err := os.Stat(filepath.Join(h.dataDir, recordingsDir, old)); err != nil {
		t.Errorf("recordings of %s after a dry run: %v", old, err)
	}
}

func TestPruneDeletesOldTerminalAttemptsAndTheirRecordings(t *testing.T) {
	h := newHarness(t, &fakeRunner{})
	passed := h.storedAttempt(t, store.StatusPassed, 40*24*time.Hour, "0123456789")
	errored := h.storedAttempt(t, store.StatusError, 31*24*time.Hour, "")
	recent := h.storedAttempt(t, store.StatusAbandoned, 29*24*time.Hour, "abc")
	running := h.storedAttempt(t, store.StatusRunning, 90*24*time.Hour, "live")
	provisioning := h.storedAttempt(t, store.StatusProvisioning, 90*24*time.Hour, "")

	report, err := h.Prune(t.Context(), 30*24*time.Hour, false)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if report.Attempts != 2 || report.Bytes != 10 || !slices.Equal(report.IDs, []string{passed, errored}) {
		t.Errorf("report = %+v, want %s and %s with 10 bytes", report, passed, errored)
	}
	for _, id := range []string{passed, errored} {
		if h.exists(t, id) {
			t.Errorf("attempt %s survived the prune", id)
		}
		if _, err := os.Stat(filepath.Join(h.dataDir, recordingsDir, id)); !os.IsNotExist(err) {
			t.Errorf("recordings of %s after prune = %v, want them gone", id, err)
		}
	}
	for _, id := range []string{recent, running, provisioning} {
		if !h.exists(t, id) {
			t.Errorf("attempt %s was pruned, want it kept", id)
		}
	}
	if _, err := os.Stat(filepath.Join(h.dataDir, recordingsDir, running)); err != nil {
		t.Errorf("recordings of the running attempt: %v", err)
	}

	again, err := h.Prune(t.Context(), 30*24*time.Hour, false)
	if err != nil {
		t.Fatalf("second Prune: %v", err)
	}
	if again.Attempts != 0 {
		t.Errorf("second prune removed %d attempts, want 0", again.Attempts)
	}
}

func TestPruneWalksPastOneBatch(t *testing.T) {
	h := newHarness(t, &fakeRunner{})
	for range pruneBatch + 3 {
		h.storedAttempt(t, store.StatusExpired, 48*time.Hour, "")
	}

	report, err := h.Prune(t.Context(), 24*time.Hour, false)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if report.Attempts != pruneBatch+3 {
		t.Errorf("pruned %d attempts, want %d", report.Attempts, pruneBatch+3)
	}
}

func TestPruneRejectsANonPositiveRetention(t *testing.T) {
	h := newHarness(t, &fakeRunner{})
	if _, err := h.Prune(t.Context(), 0, true); err == nil {
		t.Error("Prune(0) error = nil, want an error")
	}
}
