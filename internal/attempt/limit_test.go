package attempt

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/content/contenttest"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

func newLimitedHarness(t *testing.T, fr *fakeRunner, maxSandboxes int) *harness {
	t.Helper()
	labs, err := content.Load(contenttest.Dir())
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	return newHarnessWith(t, fr, &content.Content{Labs: labs}, maxSandboxes)
}

func (h *harness) addUser(t *testing.T, username string) string {
	t.Helper()
	user := store.User{ID: store.NewID(), Username: username, Role: store.RoleUser}
	if err := h.store.Users.Create(t.Context(), user); err != nil {
		t.Fatalf("create user %s: %v", username, err)
	}
	return user.ID
}

func TestStartRefusesWhenTheSandboxLimitIsReached(t *testing.T) {
	h := newLimitedHarness(t, &fakeRunner{steps: provisioningSteps}, 1)
	if _, err := h.Start(t.Context(), h.user, h.lab.Id, "guided"); err != nil {
		t.Fatalf("first start: %v", err)
	}

	second := h.addUser(t, "second")
	_, err := h.Start(t.Context(), second, h.lab.Id, "guided")
	var busy RunnerBusy
	if !errors.As(err, &busy) {
		t.Fatalf("second start = %v, want RunnerBusy", err)
	}
	if busy.Active != 1 || busy.Max != 1 {
		t.Errorf("busy = %+v, want 1 of 1", busy)
	}
	if !errors.Is(err, ErrRunnerBusy) {
		t.Error("RunnerBusy does not unwrap to ErrRunnerBusy")
	}
}

func TestStartLetsOnlyOneOfTwoSimultaneousCallersTakeTheLastSlot(t *testing.T) {
	h := newLimitedHarness(t, &fakeRunner{steps: provisioningSteps}, 1)
	first := h.addUser(t, "first")
	second := h.addUser(t, "second")

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		started int
		busy    int
	)
	for _, user := range []string{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := h.Start(t.Context(), user, h.lab.Id, "guided")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				started++
			case errors.Is(err, ErrRunnerBusy):
				busy++
			default:
				t.Errorf("start: %v", err)
			}
		}()
	}
	wg.Wait()

	if started != 1 || busy != 1 {
		t.Fatalf("started = %d, busy = %d, want exactly one of each", started, busy)
	}
	active, err := h.store.Attempts.CountActive(t.Context())
	if err != nil {
		t.Fatalf("count active: %v", err)
	}
	if active != 1 {
		t.Errorf("active attempts = %d, want 1", active)
	}
}

func TestForUserHidesAttemptsOfOtherUsers(t *testing.T) {
	h := newHarness(t, &fakeRunner{steps: provisioningSteps})
	view, err := h.Start(t.Context(), h.user, h.lab.Id, "guided")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	if _, err := h.ForUser(t.Context(), h.user, view.Id); err != nil {
		t.Errorf("ForUser as the owner = %v", err)
	}
	stranger := h.addUser(t, "stranger")
	if _, err := h.ForUser(t.Context(), stranger, view.Id); !errors.Is(err, ErrNotFound) {
		t.Errorf("ForUser as a stranger = %v, want ErrNotFound", err)
	}
	if _, err := h.Abandon(t.Context(), stranger, view.Id); !errors.Is(err, ErrNotFound) {
		t.Errorf("Abandon as a stranger = %v, want ErrNotFound", err)
	}
	if _, err := h.Abandon(t.Context(), h.user, view.Id); err != nil {
		t.Errorf("Abandon as the owner = %v", err)
	}
}

func TestDeleteRemovesTheAttemptAndItsRecordings(t *testing.T) {
	h := newHarness(t, &fakeRunner{steps: provisioningSteps})
	id := h.startRunning(t)

	dir := filepath.Join(h.dataDir, recordingsDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create recordings dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "web01-main.cast"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write cast: %v", err)
	}

	if err := h.Delete(t.Context(), id); !errors.Is(err, ErrNotTerminal) {
		t.Fatalf("Delete of a running attempt = %v, want ErrNotTerminal", err)
	}
	if _, err := h.AbandonAsAdmin(t.Context(), id); err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if err := h.Delete(t.Context(), id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := h.store.Attempts.Get(t.Context(), id); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("attempt row after Delete = %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("recordings dir after Delete = %v, want it gone", err)
	}
	if err := h.Delete(t.Context(), id); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete twice = %v, want ErrNotFound", err)
	}
}

func TestListHistoryIsNewestFirstAndCarriesCommandCounts(t *testing.T) {
	h := newHarness(t, &fakeRunner{steps: provisioningSteps})
	id := h.startRunning(t)
	if _, err := h.AbandonAsAdmin(t.Context(), id); err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if err := h.store.CommandLog.AppendBatch(t.Context(), []store.CommandEntry{
		{AttemptID: id, Node: "web01", TS: time.Now(), Command: "ip addr"},
	}); err != nil {
		t.Fatalf("append commands: %v", err)
	}

	items, err := h.ListHistory(t.Context(), h.user, time.Time{}, 10)
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Id != id || items[0].CommandCount != 1 {
		t.Errorf("item = %+v", items[0])
	}
	if items[0].UserID != h.user {
		t.Errorf("UserID = %q, want %q", items[0].UserID, h.user)
	}
}
