package recorder

import (
	"fmt"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/attempt"
	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

type harness struct {
	*Recorder
	svc     *attempt.Service
	store   *store.Store
	runner  *fakeRunner
	id      string
	dataDir string
	events  <-chan attempt.Event
	ticks   chan time.Time
	pulls   chan struct{}
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	labs, err := content.Load("../../content")
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	user, err := st.Users.Local(t.Context())
	if err != nil {
		t.Fatalf("local user: %v", err)
	}

	fr := newFakeRunner()
	svc := attempt.New(attempt.Deps{
		Store:    st,
		Runner:   fr,
		Content:  &content.Content{Labs: labs},
		Image:    "nsl/node",
		RunnerID: "fake",
		Logger:   discardLogger(),
	})
	t.Cleanup(svc.Close)

	ticks := make(chan time.Time)
	pulls := make(chan struct{}, 8)

	rec := New(Deps{Store: st, Runner: fr, Attempts: svc, DataDir: dataDir, Logger: discardLogger()})
	rec.ticker = func(time.Duration) (<-chan time.Time, func()) { return ticks, func() {} }
	rec.pulled = func() {
		select {
		case pulls <- struct{}{}:
		default:
		}
	}
	t.Cleanup(rec.Close)

	events, unsubscribe := svc.SubscribeAll()
	t.Cleanup(unsubscribe)

	rec.Start(t.Context())

	view, err := svc.Start(t.Context(), user.ID, labs[0].Id, "guided")
	if err != nil {
		t.Fatalf("start attempt: %v", err)
	}

	h := &harness{
		Recorder: rec, svc: svc, store: st, runner: fr, id: view.Id, dataDir: dataDir,
		events: events, ticks: ticks, pulls: pulls,
	}
	h.waitForStatus(t, store.StatusRunning)
	return h
}

func (h *harness) waitForStatus(t *testing.T, status string) {
	t.Helper()
	for {
		select {
		case ev, ok := <-h.events:
			if !ok {
				t.Fatal("event channel closed")
			}
			if ev.Type == attempt.EventStatus && ev.Status == status {
				return
			}
		case <-time.After(waitTimeout):
			t.Fatalf("timed out waiting for status %s", status)
		}
	}
}

func (h *harness) pull(t *testing.T) {
	t.Helper()
	select {
	case h.ticks <- time.Now():
	case <-time.After(waitTimeout):
		t.Fatal("no pull loop is receiving ticks")
	}
	h.waitForPull(t)
}

func (h *harness) waitForPull(t *testing.T) {
	t.Helper()
	select {
	case <-h.pulls:
	case <-time.After(waitTimeout):
		t.Fatal("timed out waiting for a pull to finish")
	}
}

func (h *harness) commands(t *testing.T) []store.CommandEntry {
	t.Helper()
	entries, err := h.store.CommandLog.ListByAttempt(t.Context(), h.id)
	if err != nil {
		t.Fatalf("list command log: %v", err)
	}
	return entries
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func line(ts, cmd string, exit int) string {
	return fmt.Sprintf(`{"ts":%q,"user":"nsl","cwd":"/home/nsl","cmd":%q,"exit":%d}`+"\n", ts, cmd, exit)
}

func TestPullAdvancesOffsetAndKeepsPartialLines(t *testing.T) {
	h := newHarness(t)
	first := line("2026-09-23T10:00:00.000Z", "ip link", 0)
	second := line("2026-09-23T10:00:01.000Z", "ip addr", 1)
	partial := `{"ts":"2026-09-23T10:00:02.000Z","user":"nsl"`

	h.runner.appendLines("web01", first, second, partial)
	h.pull(t)

	entries := h.commands(t)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Command != "ip link" || entries[0].Node != "web01" || entries[0].User != "nsl" || entries[0].CWD != "/home/nsl" {
		t.Errorf("first entry = %+v", entries[0])
	}
	if entries[1].Command != "ip addr" || entries[1].ExitCode != 1 {
		t.Errorf("second entry = %+v", entries[1])
	}
	if want := entries[0].TS.Format(time.RFC3339); want != "2026-09-23T10:00:00Z" {
		t.Errorf("first entry timestamp = %s", want)
	}

	h.runner.appendLines("web01", `,"cwd":"/tmp","cmd":"ping db01","exit":0}`+"\n")
	h.pull(t)

	entries = h.commands(t)
	if len(entries) != 3 {
		t.Fatalf("got %d entries after the second pull, want 3", len(entries))
	}
	if entries[2].Command != "ping db01" || entries[2].CWD != "/tmp" {
		t.Errorf("third entry = %+v", entries[2])
	}

	want := []int64{1, int64(len(first)+len(second)) + 1}
	if got := h.runner.offsets("web01"); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("tail offsets = %v, want %v", got, want)
	}
}

func TestPullSkipsUnparseableLines(t *testing.T) {
	h := newHarness(t)
	h.runner.appendLines("web01",
		line("2026-09-23T10:00:00.000Z", "ip link", 0),
		"not json at all\n",
		`{"ts":"nonsense","cmd":"x","exit":0}`+"\n",
		line("2026-09-23T10:00:03.000Z", "ip route", 0),
	)
	h.pull(t)

	entries := h.commands(t)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Command != "ip link" || entries[1].Command != "ip route" {
		t.Errorf("entries = %+v", entries)
	}
}

func TestFinalPullOnTerminalStatus(t *testing.T) {
	h := newHarness(t)
	h.runner.appendLines("web01", line("2026-09-23T10:00:00.000Z", "ip link", 0))
	h.pull(t)

	h.runner.appendLines("web01", line("2026-09-23T10:00:05.000Z", "ip link set eth1 up", 0))
	if _, err := h.svc.Abandon(t.Context(), h.id); err != nil {
		t.Fatalf("abandon: %v", err)
	}
	h.waitForPull(t)

	entries := h.commands(t)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[1].Command != "ip link set eth1 up" {
		t.Errorf("last entry = %+v", entries[1])
	}

	select {
	case h.ticks <- time.Now():
		t.Fatal("the pull loop is still running after the attempt ended")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestFinalPullRunsBeforeTheSandboxIsDestroyed(t *testing.T) {
	h := newHarness(t)
	h.runner.appendLines("web01", line("2026-09-23T10:00:00.000Z", "ip -br link show eth1", 0))
	h.pull(t)

	h.runner.appendLines("web01", line("2026-09-23T10:00:05.000Z", "sudo ip link set eth1 up", 0))
	if err := h.svc.Finish(t.Context(), h.id, store.StatusPassed); err != nil {
		t.Fatalf("finish: %v", err)
	}
	waitUntil(t, "the sandbox to be destroyed", func() bool { return h.runner.destroys() == 1 })

	entries := h.commands(t)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[1].Command != "sudo ip link set eth1 up" {
		t.Errorf("last entry = %+v", entries[1])
	}
}

func TestCloseStopsPullLoops(t *testing.T) {
	h := newHarness(t)
	h.runner.appendLines("web01", line("2026-09-23T10:00:00.000Z", "ip link", 0))
	h.pull(t)

	h.Close()

	select {
	case h.ticks <- time.Now():
		t.Fatal("a pull loop is still receiving ticks after Close")
	case <-time.After(100 * time.Millisecond):
	}
}
