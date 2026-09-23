package attempt

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

const (
	testIdleTimeout  = 5 * time.Minute
	testTickInterval = 10 * time.Second
	testHookTimeout  = 100 * time.Millisecond
)

var provisioningSteps = []string{"networks", "containers", "bootstrap", "setup"}

type harness struct {
	*Service
	clock  *fakeClock
	runner *fakeRunner
	store  *store.Store
	lab    content.Lab
	user   string
	events <-chan Event
}

func newHarness(t *testing.T, fr *fakeRunner) *harness {
	t.Helper()
	return newHarnessWithContent(t, fr, "../../content")
}

func newHarnessWithContent(t *testing.T, fr *fakeRunner, dir string) *harness {
	t.Helper()

	labs, err := content.Load(dir)
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	return newHarnessFor(t, fr, &content.Content{Labs: labs})
}

func newHarnessFor(t *testing.T, fr *fakeRunner, loaded *content.Content) *harness {
	t.Helper()

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	user, err := st.Users.Local(t.Context())
	if err != nil {
		t.Fatalf("local user: %v", err)
	}

	clock := newClock()
	svc := New(Deps{
		Store:        st,
		Runner:       fr,
		Content:      loaded,
		Image:        "nsl/node",
		RunnerID:     "fake",
		IdleTimeout:  testIdleTimeout,
		TickInterval: testTickInterval,
		HookTimeout:  testHookTimeout,
		Now:          clock.Now,
		After:        clock.After,
		Logger:       discardLogger(),
	})
	t.Cleanup(svc.Close)

	events, unsubscribe := svc.SubscribeAll()
	t.Cleanup(unsubscribe)

	return &harness{Service: svc, clock: clock, runner: fr, store: st, lab: loaded.Labs[0], user: user.ID, events: events}
}

func (h *harness) next(t *testing.T) Event {
	t.Helper()
	select {
	case ev, ok := <-h.events:
		if !ok {
			t.Fatal("event channel closed")
		}
		return ev
	case <-time.After(waitTimeout):
		t.Fatal("timed out waiting for an event")
		return Event{}
	}
}

func (h *harness) waitForStatus(t *testing.T, status string) Event {
	t.Helper()
	for {
		ev := h.next(t)
		if ev.Type == EventStatus && ev.Status == status {
			return ev
		}
	}
}

func (h *harness) startRunning(t *testing.T) string {
	t.Helper()
	view, err := h.Start(t.Context(), h.user, h.lab.Id, "guided")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	h.waitForStatus(t, store.StatusRunning)
	return view.Id
}

func (h *harness) attempt(t *testing.T, id string) store.Attempt {
	t.Helper()
	att, err := h.store.Attempts.Get(t.Context(), id)
	if err != nil {
		t.Fatalf("get attempt: %v", err)
	}
	return att
}

func TestStartProvisionsThenRuns(t *testing.T) {
	fr := &fakeRunner{steps: provisioningSteps}
	h := newHarness(t, fr)

	view, err := h.Start(t.Context(), h.user, h.lab.Id, "guided")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if view.Status != store.StatusProvisioning {
		t.Fatalf("status = %q, want provisioning", view.Status)
	}
	if got := view.Ticket("zh"); !strings.Contains(got, "web01") || strings.Contains(got, "{{") {
		t.Fatalf("ticket(zh) = %q, want the params rendered", got)
	}
	if len(view.Nodes) != 2 || view.Nodes[0].Name != "db01" {
		t.Fatalf("nodes = %+v", view.Nodes)
	}
	if len(view.Checkpoints) != 2 || view.Checkpoints[0].Status != store.CheckpointPending {
		t.Fatalf("checkpoints = %+v", view.Checkpoints)
	}
	if got := view.Checkpoints[0].Title.Get("zh"); got != "eth1 已 UP" {
		t.Fatalf("checkpoint title = %q", got)
	}

	if ev := h.next(t); ev.Type != EventStatus || ev.Status != store.StatusProvisioning || ev.AttemptID != view.Id {
		t.Fatalf("first event = %+v, want a provisioning status", ev)
	}
	for _, step := range provisioningSteps {
		ev := h.next(t)
		if ev.Type != EventProvisioning || ev.Step != step {
			t.Fatalf("event = %+v, want provisioning step %q", ev, step)
		}
	}
	if ev := h.next(t); ev.Type != EventStatus || ev.Status != store.StatusRunning {
		t.Fatalf("event = %+v, want a running status", ev)
	}

	att := h.attempt(t, view.Id)
	if att.Status != store.StatusRunning {
		t.Fatalf("stored status = %q", att.Status)
	}
	if att.SandboxID != view.Id || att.RunnerID != "fake" {
		t.Fatalf("stored sandbox = %q, runner = %q", att.SandboxID, att.RunnerID)
	}
	if att.StartedAt == nil || !att.StartedAt.Equal(h.clock.Now()) {
		t.Fatalf("stored started_at = %v", att.StartedAt)
	}
	if att.LabVersion != h.lab.Version || att.Mode != "guided" {
		t.Fatalf("stored lab_version = %d, mode = %q", att.LabVersion, att.Mode)
	}

	h.clock.advance(30 * time.Second)
	running, err := h.Get(t.Context(), view.Id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if running.ElapsedMS != 30_000 {
		t.Fatalf("elapsed_ms = %d, want 30000", running.ElapsedMS)
	}
}

func TestStartProvisioningFailureEndsInError(t *testing.T) {
	fr := &fakeRunner{err: errors.New("bootstrap: node web01 exited with 1")}
	h := newHarness(t, fr)

	view, err := h.Start(t.Context(), h.user, h.lab.Id, "guided")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	ev := h.waitForStatus(t, store.StatusError)
	if !strings.Contains(ev.ErrorMessage, "exited with 1") {
		t.Fatalf("error message = %q", ev.ErrorMessage)
	}

	att := h.attempt(t, view.Id)
	if att.Status != store.StatusError || att.EndedAt == nil {
		t.Fatalf("stored attempt = %+v", att)
	}
	if !strings.Contains(att.ErrorMessage, "exited with 1") {
		t.Fatalf("stored error message = %q", att.ErrorMessage)
	}

	waitUntil(t, "the sandbox to be destroyed", func() bool { return len(fr.destroys()) == 1 })
	if got := fr.destroys()[0]; got != view.Id {
		t.Fatalf("destroyed %q, want %q", got, view.Id)
	}
}

func TestStartRejectsSecondActiveAttempt(t *testing.T) {
	fr := &fakeRunner{block: make(chan struct{})}
	h := newHarness(t, fr)

	if _, err := h.Start(t.Context(), h.user, h.lab.Id, "guided"); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitUntil(t, "provisioning to start", func() bool { return fr.provisionCount() == 1 })

	if _, err := h.Start(t.Context(), h.user, h.lab.Id, "guided"); !errors.Is(err, ErrActiveAttempt) {
		t.Fatalf("second start error = %v, want ErrActiveAttempt", err)
	}
}

func TestStartRejectsUnknownLabAndMode(t *testing.T) {
	h := newHarness(t, &fakeRunner{})

	if _, err := h.Start(t.Context(), h.user, "no-such-lab", "guided"); !errors.Is(err, ErrUnknownLab) {
		t.Fatalf("error = %v, want ErrUnknownLab", err)
	}
	if _, err := h.Start(t.Context(), h.user, h.lab.Id, "exam"); !errors.Is(err, ErrModeNotAllowed) {
		t.Fatalf("error = %v, want ErrModeNotAllowed", err)
	}
}

func TestAbandonEndsRunningAttempt(t *testing.T) {
	fr := &fakeRunner{}
	h := newHarness(t, fr)
	id := h.startRunning(t)

	h.clock.advance(90 * time.Second)
	view, err := h.Abandon(t.Context(), id)
	if err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if view.Status != store.StatusAbandoned || view.ElapsedMS != 90_000 || view.EndedAt == nil {
		t.Fatalf("view = %+v", view)
	}

	att := h.attempt(t, id)
	if att.Status != store.StatusAbandoned || att.ElapsedMS != 90_000 || att.EndedAt == nil {
		t.Fatalf("stored attempt = %+v", att)
	}

	waitUntil(t, "the sandbox to be destroyed", func() bool { return len(fr.destroys()) == 1 })

	if _, err := h.Abandon(t.Context(), id); !errors.Is(err, ErrTerminal) {
		t.Fatalf("second abandon error = %v, want ErrTerminal", err)
	}
	if got := fr.destroys(); len(got) != 1 {
		t.Fatalf("destroys = %v, want exactly one", got)
	}
}

func TestFinishPassed(t *testing.T) {
	h := newHarness(t, &fakeRunner{})
	id := h.startRunning(t)

	h.clock.advance(time.Minute)
	if err := h.Finish(t.Context(), id, store.StatusPassed); err != nil {
		t.Fatalf("finish: %v", err)
	}

	att := h.attempt(t, id)
	if att.Status != store.StatusPassed || att.EndedAt == nil || att.ElapsedMS != 60_000 {
		t.Fatalf("stored attempt = %+v", att)
	}
	if ev := h.waitForStatus(t, store.StatusPassed); ev.ElapsedMS != 60_000 {
		t.Fatalf("status event = %+v", ev)
	}

	if err := h.Finish(t.Context(), id, store.StatusPassed); !errors.Is(err, ErrTerminal) {
		t.Fatalf("second finish error = %v, want ErrTerminal", err)
	}
}

func TestFinishUnknownAttempt(t *testing.T) {
	h := newHarness(t, &fakeRunner{})
	if err := h.Finish(t.Context(), "01K0000000000000000000000", store.StatusPassed); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestCurrent(t *testing.T) {
	h := newHarness(t, &fakeRunner{})

	if _, ok, err := h.Current(t.Context(), h.user); err != nil || ok {
		t.Fatalf("current = %v, %v, want no attempt", ok, err)
	}

	id := h.startRunning(t)
	view, ok, err := h.Current(t.Context(), h.user)
	if err != nil || !ok || view.Id != id {
		t.Fatalf("current = %+v, %v, %v", view, ok, err)
	}

	if _, err := h.Abandon(t.Context(), id); err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if _, ok, err := h.Current(t.Context(), h.user); err != nil || ok {
		t.Fatalf("current after abandon = %v, %v", ok, err)
	}
}

func TestIdleTimeoutExpiresAttempt(t *testing.T) {
	fr := &fakeRunner{}
	h := newHarness(t, fr)
	id := h.startRunning(t)

	h.Connected(id)
	h.Disconnected(id)
	h.clock.advance(testIdleTimeout)
	h.clock.fire(t, testIdleTimeout)

	h.waitForStatus(t, store.StatusExpired)
	att := h.attempt(t, id)
	if att.Status != store.StatusExpired || att.EndedAt == nil {
		t.Fatalf("stored attempt = %+v", att)
	}
	waitUntil(t, "the sandbox to be destroyed", func() bool { return len(fr.destroys()) == 1 })
}

func TestReconnectCancelsIdleTimeout(t *testing.T) {
	h := newHarness(t, &fakeRunner{})
	id := h.startRunning(t)

	h.Connected(id)
	h.Disconnected(id)
	h.Connected(id)
	h.clock.fire(t, testIdleTimeout)

	select {
	case ev := <-h.events:
		t.Fatalf("unexpected event %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
	if att := h.attempt(t, id); att.Status != store.StatusRunning {
		t.Fatalf("stored status = %q, want running", att.Status)
	}
}

func TestTicksStopAtTerminalStatus(t *testing.T) {
	h := newHarness(t, &fakeRunner{})
	id := h.startRunning(t)

	h.clock.advance(testTickInterval)
	h.clock.fire(t, testTickInterval)
	ev := h.next(t)
	if ev.Type != EventTick || ev.AttemptID != id || ev.ElapsedMS != testTickInterval.Milliseconds() {
		t.Fatalf("event = %+v, want a tick", ev)
	}

	h.clock.advance(testTickInterval)
	h.clock.fire(t, testTickInterval)
	if ev := h.next(t); ev.Type != EventTick || ev.ElapsedMS != 2*testTickInterval.Milliseconds() {
		t.Fatalf("event = %+v, want a second tick", ev)
	}

	if _, err := h.Abandon(t.Context(), id); err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if ev := h.waitForStatus(t, store.StatusAbandoned); ev.AttemptID != id {
		t.Fatalf("event = %+v", ev)
	}

	h.clock.fire(t, testTickInterval)
	select {
	case ev := <-h.events:
		t.Fatalf("unexpected event %+v after the attempt ended", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestRecoverExpiresStaleAttemptsAndCollects(t *testing.T) {
	fr := &fakeRunner{}
	h := newHarness(t, fr)
	ctx := t.Context()

	startedAt := h.clock.Now().Add(-2 * time.Minute)
	stale := store.Attempt{
		ID: store.NewID(), UserID: h.user, LabID: h.lab.Id, LabVersion: h.lab.Version,
		Mode: "guided", ParamsJSON: "{}", Status: store.StatusRunning,
		StartedAt: &startedAt, CreatedAt: startedAt,
	}
	provisioning := store.Attempt{
		ID: store.NewID(), UserID: h.user, LabID: h.lab.Id, LabVersion: h.lab.Version,
		Mode: "guided", ParamsJSON: "{}", Status: store.StatusProvisioning, CreatedAt: h.clock.Now(),
	}
	for _, att := range []store.Attempt{stale, provisioning} {
		if err := h.store.Attempts.Create(ctx, att); err != nil {
			t.Fatalf("create attempt: %v", err)
		}
	}

	if err := h.Recover(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}

	if att := h.attempt(t, stale.ID); att.Status != store.StatusExpired || att.ElapsedMS != 120_000 || att.EndedAt == nil {
		t.Fatalf("stale attempt = %+v", att)
	}
	if att := h.attempt(t, provisioning.ID); att.Status != store.StatusExpired || att.ElapsedMS != 0 {
		t.Fatalf("provisioning attempt = %+v", att)
	}
	if fr.gcCount() != 1 {
		t.Fatalf("gc calls = %d, want 1", fr.gcCount())
	}
}

func TestCloseStopsEverything(t *testing.T) {
	fr := &fakeRunner{block: make(chan struct{})}
	h := newHarness(t, fr)

	if _, err := h.Start(t.Context(), h.user, h.lab.Id, "guided"); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitUntil(t, "provisioning to start", func() bool { return fr.provisionCount() == 1 })

	done := make(chan struct{})
	go func() {
		h.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(waitTimeout):
		t.Fatal("Close did not return")
	}

	for {
		select {
		case _, ok := <-h.events:
			if !ok {
				return
			}
		case <-time.After(waitTimeout):
			t.Fatal("event channel was not closed by Close")
		}
	}
}

func TestPublishCheckpoint(t *testing.T) {
	h := newHarness(t, &fakeRunner{})
	passed := h.clock.Now()

	h.PublishCheckpoint("att", CheckpointEvent{Id: "link-up", Status: store.CheckpointPass, FirstPassedAt: &passed})

	ev := h.next(t)
	if ev.Type != EventCheckpoint || ev.AttemptID != "att" || ev.Checkpoint == nil {
		t.Fatalf("event = %+v", ev)
	}
	if ev.Checkpoint.Id != "link-up" || ev.Checkpoint.Status != store.CheckpointPass {
		t.Fatalf("checkpoint = %+v", ev.Checkpoint)
	}
}

func TestGetUnknownAttempt(t *testing.T) {
	h := newHarness(t, &fakeRunner{})
	if _, err := h.Get(context.Background(), "01K0000000000000000000000"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestBeforeDestroyHooksRunBeforeDestroy(t *testing.T) {
	cases := []struct {
		name   string
		status string
		end    func(t *testing.T, h *harness, id string)
	}{
		{"finish", store.StatusPassed, func(t *testing.T, h *harness, id string) {
			if err := h.Finish(t.Context(), id, store.StatusPassed); err != nil {
				t.Fatalf("finish: %v", err)
			}
		}},
		{"abandon", store.StatusAbandoned, func(t *testing.T, h *harness, id string) {
			if _, err := h.Abandon(t.Context(), id); err != nil {
				t.Fatalf("abandon: %v", err)
			}
		}},
		{"expiry", store.StatusExpired, func(t *testing.T, h *harness, id string) {
			h.Connected(id)
			h.Disconnected(id)
			h.clock.advance(testIdleTimeout)
			h.clock.fire(t, testIdleTimeout)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fr := &fakeRunner{}
			h := newHarness(t, fr)

			var (
				mu        sync.Mutex
				order     []string
				views     []View
				destroyed []string
			)
			record := func(name string) BeforeDestroyFunc {
				return func(_ context.Context, v View) {
					mu.Lock()
					defer mu.Unlock()
					order = append(order, name)
					views = append(views, v)
					destroyed = append(destroyed, fr.destroys()...)
				}
			}
			h.OnBeforeDestroy(record("first"))
			h.OnBeforeDestroy(record("second"))

			id := h.startRunning(t)
			tc.end(t, h, id)
			waitUntil(t, "the sandbox to be destroyed", func() bool { return len(fr.destroys()) == 1 })

			mu.Lock()
			defer mu.Unlock()
			if len(order) != 2 || order[0] != "first" || order[1] != "second" {
				t.Fatalf("hooks ran as %v, want them in registration order", order)
			}
			if len(destroyed) != 0 {
				t.Fatalf("the sandbox was already destroyed when a hook ran: %v", destroyed)
			}
			for _, v := range views {
				if v.Id != id || v.Status != tc.status || v.SandboxID != id {
					t.Fatalf("hook view = %+v, want attempt %s as %s", v, id, tc.status)
				}
			}
		})
	}
}

func TestBeforeDestroyHookDeadlineDoesNotBlockDestroy(t *testing.T) {
	fr := &fakeRunner{}
	h := newHarness(t, fr)

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	blocked := make(chan struct{})
	h.OnBeforeDestroy(func(context.Context, View) {
		close(blocked)
		<-release
	})

	id := h.startRunning(t)
	if _, err := h.Abandon(t.Context(), id); err != nil {
		t.Fatalf("abandon: %v", err)
	}

	select {
	case <-blocked:
	case <-time.After(waitTimeout):
		t.Fatal("the hook never ran")
	}
	waitUntil(t, "the sandbox to be destroyed", func() bool { return len(fr.destroys()) == 1 })
}

func TestBeforeDestroyHookPanicDoesNotBlockDestroy(t *testing.T) {
	fr := &fakeRunner{}
	h := newHarness(t, fr)

	var ran atomic.Int64
	h.OnBeforeDestroy(func(context.Context, View) { panic("boom") })
	h.OnBeforeDestroy(func(context.Context, View) { ran.Add(1) })

	id := h.startRunning(t)
	if _, err := h.Abandon(t.Context(), id); err != nil {
		t.Fatalf("abandon: %v", err)
	}

	waitUntil(t, "the sandbox to be destroyed", func() bool { return len(fr.destroys()) == 1 })
	if ran.Load() != 1 {
		t.Fatalf("the hook after the panicking one ran %d times, want 1", ran.Load())
	}
}

func TestRecoverDoesNotRunBeforeDestroyHooks(t *testing.T) {
	h := newHarness(t, &fakeRunner{})

	var ran atomic.Int64
	h.OnBeforeDestroy(func(context.Context, View) { ran.Add(1) })

	startedAt := h.clock.Now().Add(-time.Minute)
	stale := store.Attempt{
		ID: store.NewID(), UserID: h.user, LabID: h.lab.Id, LabVersion: h.lab.Version,
		Mode: "guided", ParamsJSON: "{}", Status: store.StatusRunning,
		StartedAt: &startedAt, CreatedAt: startedAt,
	}
	if err := h.store.Attempts.Create(t.Context(), stale); err != nil {
		t.Fatalf("create attempt: %v", err)
	}
	if err := h.Recover(t.Context()); err != nil {
		t.Fatalf("recover: %v", err)
	}

	if ran.Load() != 0 {
		t.Fatalf("recover ran %d hooks, want none", ran.Load())
	}
}
