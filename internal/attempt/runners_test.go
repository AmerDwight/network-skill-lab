package attempt

import (
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/content/contenttest"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

const remoteName = "lab-01"

type fakeRemotes struct {
	mu        sync.Mutex
	runner    *fakeRunner
	capacity  int
	connected bool
}

func (f *fakeRemotes) Members() []runner.Member {
	f.mu.Lock()
	defer f.mu.Unlock()
	return []runner.Member{{Name: remoteName, Capabilities: []runner.Env{runner.EnvContainer}, Capacity: f.capacity, Connected: f.connected}}
}

func (f *fakeRemotes) Get(name string) (runner.Runner, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if name != remoteName || !f.connected {
		return nil, false
	}
	return f.runner, true
}

func (f *fakeRemotes) setConnected(connected bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connected = connected
}

func newRegistryHarness(t *testing.T, reg *runner.Registry) *harness {
	t.Helper()
	labs, err := content.Load(contenttest.Dir())
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

	clock := newClock()
	svc := New(Deps{
		Store:        st,
		Runners:      reg,
		Content:      &content.Content{Labs: labs},
		Image:        "nsl/node",
		DataDir:      dataDir,
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
	return &harness{Service: svc, clock: clock, store: st, dataDir: dataDir, lab: labs[0], user: user.ID, events: events}
}

type twoRunners struct {
	*harness
	local   *fakeRunner
	remote  *fakeRunner
	remotes *fakeRemotes
}

func newTwoRunners(t *testing.T) twoRunners {
	t.Helper()
	local := &fakeRunner{steps: provisioningSteps}
	remote := &fakeRunner{steps: provisioningSteps}
	remotes := &fakeRemotes{runner: remote, capacity: 1, connected: true}
	h := newRegistryHarness(t, runner.NewRegistry(local, 1, remotes))
	return twoRunners{harness: h, local: local, remote: remote, remotes: remotes}
}

func (h twoRunners) startFor(t *testing.T, username string) View {
	t.Helper()
	view, err := h.Start(t.Context(), h.addUser(t, username), h.lab.Id, "guided")
	if err != nil {
		t.Fatalf("start for %s: %v", username, err)
	}
	waitUntil(t, username+"'s attempt to run", func() bool {
		return h.attempt(t, view.Id).Status == store.StatusRunning
	})
	return view
}

func TestStartSpreadsAttemptsAcrossRunners(t *testing.T) {
	h := newTwoRunners(t)

	first := h.startFor(t, "first")
	second := h.startFor(t, "second")
	if first.Runner != remoteName || second.Runner != runner.LocalName {
		t.Fatalf("runners = %q, %q, want %s then %s", first.Runner, second.Runner, remoteName, runner.LocalName)
	}
	if got := h.attempt(t, first.Id).RunnerID; got != remoteName {
		t.Errorf("stored runner of the first attempt = %q", got)
	}
	if h.remote.provisionCount() != 1 || h.local.provisionCount() != 1 {
		t.Errorf("provisions: remote %d, local %d, want one each", h.remote.provisionCount(), h.local.provisionCount())
	}

	_, err := h.Start(t.Context(), h.addUser(t, "third"), h.lab.Id, "guided")
	var busy RunnerBusy
	if !errors.As(err, &busy) {
		t.Fatalf("third start = %v, want RunnerBusy", err)
	}
	want := []runner.Load{{Name: remoteName, Active: 1, Capacity: 1}, {Name: runner.LocalName, Active: 1, Capacity: 1}}
	if busy.Active != 2 || busy.Max != 2 || !reflect.DeepEqual(busy.Runners, want) {
		t.Errorf("busy = %+v, want 2 of 2 with %+v", busy, want)
	}

	statuses, err := h.RunnerStatuses(t.Context())
	if err != nil {
		t.Fatalf("RunnerStatuses() error = %v", err)
	}
	if len(statuses) != 2 || statuses[0].Name != runner.LocalName || statuses[0].Active != 1 || statuses[1].Active != 1 {
		t.Errorf("statuses = %+v", statuses)
	}
}

func TestRunnerRoutesSandboxCallsToTheirRunner(t *testing.T) {
	h := newTwoRunners(t)
	h.remote.exec = func(execCall, int) (runner.ExecResult, error) {
		return runner.ExecResult{Stdout: []byte("remote")}, nil
	}
	h.local.exec = func(execCall, int) (runner.ExecResult, error) { return runner.ExecResult{Stdout: []byte("local")}, nil }

	onRemote := h.startFor(t, "first")
	onLocal := h.startFor(t, "second")

	routed := h.Runner()
	for _, tc := range []struct{ view View }{{onRemote}, {onLocal}} {
		view, err := h.Get(t.Context(), tc.view.Id)
		if err != nil {
			t.Fatal(err)
		}
		result, err := routed.Exec(t.Context(), runner.SandboxID(view.SandboxID), "web01", []string{"true"}, runner.ExecOptions{})
		if err != nil {
			t.Fatalf("Exec() error = %v", err)
		}
		if want := map[string]string{remoteName: "remote", runner.LocalName: "local"}[view.Runner]; string(result.Stdout) != want {
			t.Errorf("Exec on %s's sandbox reached %q", view.Runner, result.Stdout)
		}
	}
	if _, err := routed.Exec(t.Context(), "unknown", "web01", nil, runner.ExecOptions{}); !errors.Is(err, runner.ErrSandboxNotFound) {
		t.Errorf("Exec on an unknown sandbox = %v, want ErrSandboxNotFound", err)
	}
	if _, err := routed.Provision(t.Context(), runner.SandboxSpec{}); err == nil {
		t.Error("Provision through the routed runner succeeded")
	}
}

func TestRunnerDisconnectedEndsOnlyItsAttempts(t *testing.T) {
	h := newTwoRunners(t)
	onRemote := h.startFor(t, "first")
	onLocal := h.startFor(t, "second")

	h.remotes.setConnected(false)
	h.RunnerDisconnected(remoteName)

	waitUntil(t, "the remote attempt to end", func() bool {
		return h.attempt(t, onRemote.Id).Status == store.StatusError
	})
	if got := h.attempt(t, onRemote.Id).ErrorMessage; got != ReasonRunnerDisconnected {
		t.Errorf("error message = %q, want %q", got, ReasonRunnerDisconnected)
	}
	if got := h.attempt(t, onLocal.Id).Status; got != store.StatusRunning {
		t.Errorf("local attempt status = %q, want running", got)
	}
	if _, err := h.Runner().Exec(t.Context(), runner.SandboxID(onRemote.Id), "web01", nil, runner.ExecOptions{}); err == nil {
		t.Error("the ended attempt's sandbox is still routable")
	}
	if got := h.remote.destroys(); len(got) != 0 {
		t.Errorf("destroy reached a disconnected runner: %v", got)
	}

	if _, err := h.Start(t.Context(), h.addUser(t, "third"), h.lab.Id, "guided"); !errors.Is(err, ErrRunnerBusy) {
		t.Errorf("start with only the full local runner connected = %v, want busy", err)
	}
}

func TestRunnerDisconnectedDuringProvisioning(t *testing.T) {
	h := newTwoRunners(t)
	h.remote.block = make(chan struct{})

	view, err := h.Start(t.Context(), h.addUser(t, "first"), h.lab.Id, "guided")
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the provision call", func() bool { return h.remote.provisionCount() == 1 })
	h.remotes.setConnected(false)
	h.RunnerDisconnected(remoteName)
	waitUntil(t, "the attempt to end", func() bool { return h.attempt(t, view.Id).Status == store.StatusError })
	close(h.remote.block)

	if got := h.attempt(t, view.Id).ErrorMessage; got != ReasonRunnerDisconnected {
		t.Errorf("error message = %q", got)
	}
}

func TestRunnerReconnectedDestroysLeftovers(t *testing.T) {
	h := newTwoRunners(t)
	h.RunnerReconnected(remoteName, []string{"sb-1", "sb-2"})
	waitUntil(t, "the leftovers to be destroyed", func() bool { return len(h.remote.destroys()) == 2 })
	if got := h.remote.destroys(); !slices.Equal(got, []string{"sb-1", "sb-2"}) {
		t.Errorf("destroyed %v", got)
	}
	if got := h.local.destroys(); len(got) != 0 {
		t.Errorf("local runner destroyed %v", got)
	}
}

func TestStartWithoutACapableRunner(t *testing.T) {
	h := newRegistryHarness(t, runner.NewRegistry(nil, 3, nil))
	_, err := h.Start(t.Context(), h.user, h.lab.Id, "guided")
	if !errors.Is(err, runner.ErrNoRunner) {
		t.Fatalf("start = %v, want ErrNoRunner", err)
	}
	if health := h.Runner().Health(t.Context()); health.OK || health.Error == "" {
		t.Errorf("health without runners = %+v", health)
	}
}

func TestRecoverCollectsOnEveryConnectedRunner(t *testing.T) {
	h := newTwoRunners(t)
	if err := h.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if h.local.gcCount() != 1 || h.remote.gcCount() != 1 {
		t.Errorf("gc: local %d, remote %d, want one each", h.local.gcCount(), h.remote.gcCount())
	}

	h.remotes.setConnected(false)
	if err := h.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if h.remote.gcCount() != 1 {
		t.Errorf("gc reached a disconnected runner")
	}
}
