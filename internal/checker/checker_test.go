package checker

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/attempt"
	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

type harness struct {
	*Checker
	svc    *attempt.Service
	store  *store.Store
	runner *fakeRunner
	lab    content.Lab
	id     string
	events <-chan attempt.Event
	ticks  chan time.Time
	swepts chan struct{}
	loops  *atomic.Int64
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	labs, err := content.Load("../../content")
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	st, err := store.Open(t.TempDir())
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
		Labs:     labs,
		Image:    "nsl/node",
		RunnerID: "fake",
		Logger:   discardLogger(),
	})
	t.Cleanup(svc.Close)

	ticks := make(chan time.Time)
	swepts := make(chan struct{}, 8)
	var loops atomic.Int64

	chk := New(Deps{Store: st, Runner: fr, Attempts: svc, Timeout: time.Second, Logger: discardLogger()})
	chk.ticker = func(time.Duration) (<-chan time.Time, func()) {
		loops.Add(1)
		return ticks, func() {}
	}
	chk.swept = func() {
		select {
		case swepts <- struct{}{}:
		default:
		}
	}
	t.Cleanup(chk.Close)

	events, unsubscribe := svc.SubscribeAll()
	t.Cleanup(unsubscribe)

	chk.Start(t.Context())

	view, err := svc.Start(t.Context(), user.ID, labs[0].Id, "guided")
	if err != nil {
		t.Fatalf("start attempt: %v", err)
	}

	h := &harness{
		Checker: chk, svc: svc, store: st, runner: fr, lab: labs[0], id: view.Id,
		events: events, ticks: ticks, swepts: swepts, loops: &loops,
	}
	h.waitForStatus(t, store.StatusRunning)
	return h
}

func (h *harness) waitForStatus(t *testing.T, status string) {
	t.Helper()
	for {
		ev := h.next(t)
		if ev.Type == attempt.EventStatus && ev.Status == status {
			return
		}
	}
}

func (h *harness) next(t *testing.T) attempt.Event {
	t.Helper()
	select {
	case ev, ok := <-h.events:
		if !ok {
			t.Fatal("event channel closed")
		}
		return ev
	case <-time.After(waitTimeout):
		t.Fatal("timed out waiting for an event")
		return attempt.Event{}
	}
}

func (h *harness) nextCheckpoint(t *testing.T) attempt.CheckpointEvent {
	t.Helper()
	for {
		ev := h.next(t)
		if ev.Type == attempt.EventCheckpoint {
			return *ev.Checkpoint
		}
	}
}

func (h *harness) key(t *testing.T, checkpointID string) string {
	t.Helper()
	for _, cp := range h.lab.Checkpoints {
		if cp.Id != checkpointID {
			continue
		}
		script, err := os.ReadFile(filepath.Join(h.lab.Dir, cp.Script))
		if err != nil {
			t.Fatalf("read checkpoint script %s: %v", cp.Script, err)
		}
		return execKey(cp.Node, script)
	}
	t.Fatalf("lab %s has no checkpoint %s", h.lab.Id, checkpointID)
	return ""
}

func (h *harness) program(t *testing.T, checkpointID string, outcomes ...outcome) {
	t.Helper()
	h.runner.program(h.key(t, checkpointID), outcomes...)
}

func (h *harness) sendTick(t *testing.T) {
	t.Helper()
	select {
	case h.ticks <- time.Now():
	case <-time.After(waitTimeout):
		t.Fatal("no sweep loop is receiving ticks")
	}
}

func (h *harness) sweep(t *testing.T) {
	t.Helper()
	h.sendTick(t)
	select {
	case <-h.swepts:
	case <-time.After(waitTimeout):
		t.Fatal("timed out waiting for a sweep to finish")
	}
}

func (h *harness) runs(t *testing.T) map[string]store.CheckpointRun {
	t.Helper()
	runs, err := h.store.CheckpointRuns.ListByAttempt(t.Context(), h.id)
	if err != nil {
		t.Fatalf("list checkpoint runs: %v", err)
	}
	byID := make(map[string]store.CheckpointRun, len(runs))
	for _, run := range runs {
		byID[run.CheckpointID] = run
	}
	return byID
}

func (h *harness) status(t *testing.T) string {
	t.Helper()
	att, err := h.store.Attempts.Get(t.Context(), h.id)
	if err != nil {
		t.Fatalf("get attempt: %v", err)
	}
	return att.Status
}

func TestFirstSweepPublishesEveryCheckpoint(t *testing.T) {
	h := newHarness(t)
	h.program(t, "link-up", pass())
	h.program(t, "ping-peer", fail())

	h.sweep(t)

	first := h.nextCheckpoint(t)
	if first.Id != "link-up" || first.Status != store.CheckpointPass {
		t.Errorf("first event = %+v, want link-up pass", first)
	}
	if first.FirstPassedAt == nil {
		t.Error("link-up event has no first_passed_at")
	}
	second := h.nextCheckpoint(t)
	if second.Id != "ping-peer" || second.Status != store.CheckpointFail {
		t.Errorf("second event = %+v, want ping-peer fail", second)
	}
	if second.FirstPassedAt != nil {
		t.Errorf("ping-peer event has first_passed_at %v", second.FirstPassedAt)
	}

	runs := h.runs(t)
	if got := runs["link-up"]; got.LastStatus != store.CheckpointPass || got.FirstPassedAt == nil || got.LastRunAt == nil {
		t.Errorf("link-up run = %+v", got)
	}
	if got := runs["ping-peer"]; got.LastStatus != store.CheckpointFail || got.FirstPassedAt != nil {
		t.Errorf("ping-peer run = %+v", got)
	}
}

func TestExecFailureAndTimeoutMapToError(t *testing.T) {
	h := newHarness(t)
	h.program(t, "link-up", fault())
	h.program(t, "ping-peer", hang())

	h.sweep(t)

	for range 2 {
		ev := h.nextCheckpoint(t)
		if ev.Status != store.CheckpointError {
			t.Errorf("%s status = %s, want error", ev.Id, ev.Status)
		}
	}
	for id, run := range h.runs(t) {
		if run.LastStatus != store.CheckpointError {
			t.Errorf("%s run status = %s, want error", id, run.LastStatus)
		}
	}
}

func TestLaterSweepsPublishOnlyChanges(t *testing.T) {
	h := newHarness(t)
	h.program(t, "link-up", pass())
	h.program(t, "ping-peer", fail(), fail(), fault())

	h.sweep(t)
	if ev := h.nextCheckpoint(t); ev.Id != "link-up" {
		t.Fatalf("first event = %+v, want link-up", ev)
	}
	if ev := h.nextCheckpoint(t); ev.Id != "ping-peer" || ev.Status != store.CheckpointFail {
		t.Fatalf("second event = %+v, want ping-peer fail", ev)
	}

	h.sweep(t)
	h.sweep(t)

	ev := h.nextCheckpoint(t)
	if ev.Id != "ping-peer" || ev.Status != store.CheckpointError {
		t.Fatalf("event after three sweeps = %+v, want ping-peer error", ev)
	}
	if want := 6; h.runner.execs() != want {
		t.Errorf("exec count = %d, want %d", h.runner.execs(), want)
	}
}

func TestAllPassFinishesTheAttempt(t *testing.T) {
	h := newHarness(t)
	h.program(t, "link-up", pass())
	h.program(t, "ping-peer", fail(), pass())

	h.sweep(t)
	h.sendTick(t)

	h.waitForStatus(t, store.StatusPassed)
	if got := h.status(t); got != store.StatusPassed {
		t.Errorf("attempt status = %s, want passed", got)
	}

	select {
	case h.ticks <- time.Now():
		t.Fatal("the sweep loop is still running after the attempt passed")
	case <-time.After(100 * time.Millisecond):
	}
	if want := 4; h.runner.execs() != want {
		t.Errorf("exec count = %d, want %d", h.runner.execs(), want)
	}
}

func TestOverlappingSweepIsSkipped(t *testing.T) {
	h := newHarness(t)
	h.program(t, "link-up", pass())
	h.program(t, "ping-peer", fail())

	release := h.runner.blockOn(h.key(t, "link-up"))
	h.sendTick(t)

	deadline := time.Now().Add(waitTimeout)
	for h.runner.waitingOn(h.key(t, "link-up")) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the first sweep never reached the blocked checkpoint")
		}
		time.Sleep(time.Millisecond)
	}
	h.sendTick(t)
	close(release)

	select {
	case <-h.swepts:
	case <-time.After(waitTimeout):
		t.Fatal("timed out waiting for the blocked sweep to finish")
	}
	select {
	case <-h.swepts:
		t.Fatal("the tick that arrived during the sweep was queued instead of skipped")
	case <-time.After(100 * time.Millisecond):
	}
	if want := 2; h.runner.execs() != want {
		t.Errorf("exec count = %d, want %d", h.runner.execs(), want)
	}
}

func TestStartLoopIsIdempotent(t *testing.T) {
	h := newHarness(t)
	h.program(t, "link-up", pass())
	h.program(t, "ping-peer", fail())
	h.sweep(t)

	h.startLoop(context.Background(), h.id)
	h.sweep(t)

	if got := h.loops.Load(); got != 1 {
		t.Errorf("sweep loops started = %d, want 1", got)
	}
	if want := 4; h.runner.execs() != want {
		t.Errorf("exec count = %d, want %d", h.runner.execs(), want)
	}
}

func TestCloseStopsSweepLoops(t *testing.T) {
	h := newHarness(t)
	h.program(t, "link-up", pass())
	h.program(t, "ping-peer", fail())
	h.sweep(t)

	h.Close()

	select {
	case h.ticks <- time.Now():
		t.Fatal("a sweep loop is still receiving ticks after Close")
	case <-time.After(100 * time.Millisecond):
	}
	if want := 2; h.runner.execs() != want {
		t.Errorf("exec count = %d, want %d", h.runner.execs(), want)
	}
}
