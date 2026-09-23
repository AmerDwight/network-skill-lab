package attempt

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

const (
	defaultTickInterval = 10 * time.Second
	defaultIdleTimeout  = 15 * time.Minute
	provisionTimeout    = 3 * time.Minute
	precheckTimeout     = 30 * time.Second
	precheckTailLines   = 20
	destroyTimeout      = 2 * time.Minute
	storeTimeout        = 10 * time.Second
	defaultHookTimeout  = 5 * time.Second
)

var (
	ErrActiveAttempt  = errors.New("user already has an active attempt")
	ErrNotFound       = errors.New("attempt not found")
	ErrTerminal       = errors.New("attempt has already ended")
	ErrNotTerminal    = errors.New("attempt has not ended yet")
	ErrProvisioning   = errors.New("attempt is still provisioning")
	ErrUnknownLab     = errors.New("unknown lab")
	ErrUnknownDoc     = errors.New("unknown doc")
	ErrModeNotAllowed = errors.New("mode not allowed by the lab")
	ErrModeNotReal    = errors.New("attempt is not in real mode")
	ErrNoSweeper      = errors.New("no checker is wired to the attempt service")
)

type Deps struct {
	Store        *store.Store
	Runner       runner.Runner
	Content      *content.Content
	Image        string
	RunnerID     string
	IdleTimeout  time.Duration
	TickInterval time.Duration
	HookTimeout  time.Duration
	Now          func() time.Time
	After        func(time.Duration) <-chan time.Time
	Logger       *slog.Logger
}

type Sweeper interface {
	SweepNow(ctx context.Context, id string) (map[string]string, error)
}

type Service struct {
	store        *store.Store
	runner       runner.Runner
	labs         map[string]content.Lab
	docs         map[string]content.Doc
	image        string
	runnerID     string
	idleTimeout  time.Duration
	tickInterval time.Duration
	hookTimeout  time.Duration
	now          func() time.Time
	after        func(time.Duration) <-chan time.Time
	log          *slog.Logger
	bus          *bus

	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce sync.Once

	mu      sync.Mutex
	tracked map[string]*tracker

	hooksMu sync.Mutex
	hooks   []BeforeDestroyFunc

	sweeperMu sync.Mutex
	sweeper   Sweeper
}

type BeforeDestroyFunc func(ctx context.Context, v View)

type tracker struct {
	conns int
	idle  chan struct{}
	stop  chan struct{}
}

func (t *tracker) disarm() {
	if t.idle != nil {
		close(t.idle)
		t.idle = nil
	}
}

func (t *tracker) close() {
	t.disarm()
	close(t.stop)
}

func New(deps Deps) *Service {
	if deps.Content == nil {
		deps.Content = &content.Content{}
	}
	labs := make(map[string]content.Lab, len(deps.Content.Labs))
	for _, lab := range deps.Content.Labs {
		labs[lab.Id] = lab
	}
	docs := make(map[string]content.Doc, len(deps.Content.Docs))
	for _, doc := range deps.Content.Docs {
		docs[doc.ID] = doc
	}
	if deps.IdleTimeout <= 0 {
		deps.IdleTimeout = defaultIdleTimeout
	}
	if deps.TickInterval <= 0 {
		deps.TickInterval = defaultTickInterval
	}
	if deps.HookTimeout <= 0 {
		deps.HookTimeout = defaultHookTimeout
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.After == nil {
		deps.After = time.After
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Service{
		store:        deps.Store,
		runner:       deps.Runner,
		labs:         labs,
		docs:         docs,
		image:        deps.Image,
		runnerID:     deps.RunnerID,
		idleTimeout:  deps.IdleTimeout,
		tickInterval: deps.TickInterval,
		hookTimeout:  deps.HookTimeout,
		now:          deps.Now,
		after:        deps.After,
		log:          deps.Logger,
		bus:          newBus(deps.Logger),
		ctx:          ctx,
		cancel:       cancel,
		tracked:      map[string]*tracker{},
	}
}

func (s *Service) Close() {
	s.closeOnce.Do(func() {
		s.cancel()
		s.mu.Lock()
		for id, t := range s.tracked {
			delete(s.tracked, id)
			t.close()
		}
		s.mu.Unlock()
		s.wg.Wait()
		s.bus.close()
	})
}

func (s *Service) SetSweeper(sweeper Sweeper) {
	s.sweeperMu.Lock()
	defer s.sweeperMu.Unlock()
	s.sweeper = sweeper
}

func (s *Service) currentSweeper() Sweeper {
	s.sweeperMu.Lock()
	defer s.sweeperMu.Unlock()
	return s.sweeper
}

func (s *Service) OnBeforeDestroy(fn BeforeDestroyFunc) {
	if fn == nil {
		return
	}
	s.hooksMu.Lock()
	defer s.hooksMu.Unlock()
	s.hooks = append(s.hooks, fn)
}

func (s *Service) Subscribe(id string) (<-chan Event, func()) {
	return s.bus.subscribe(id)
}

func (s *Service) SubscribeAll() (<-chan Event, func()) {
	return s.bus.subscribe("")
}

func (s *Service) PublishCheckpoint(id string, ev CheckpointEvent) {
	s.bus.publish(Event{AttemptID: id, Type: EventCheckpoint, ServerTime: s.now(), Checkpoint: &ev})
}

func (s *Service) Start(ctx context.Context, userID, labID, mode string) (View, error) {
	lab, ok := s.labs[labID]
	if !ok {
		return View{}, fmt.Errorf("%w: %s", ErrUnknownLab, labID)
	}
	if !slices.Contains(lab.Modes, mode) {
		return View{}, fmt.Errorf("%w: lab %s does not offer %s", ErrModeNotAllowed, labID, mode)
	}

	if _, active, err := s.store.Attempts.ActiveForUser(ctx, userID); err != nil {
		return View{}, err
	} else if active {
		return View{}, fmt.Errorf("%w: user %s", ErrActiveAttempt, userID)
	}

	seed, err := newSeed()
	if err != nil {
		return View{}, err
	}
	resolved, err := lab.ResolveFor(seed)
	if err != nil {
		return View{}, fmt.Errorf("resolve params of lab %s: %w", labID, err)
	}
	paramsJSON, err := json.Marshal(resolved.Params)
	if err != nil {
		return View{}, fmt.Errorf("encode params of lab %s: %w", labID, err)
	}

	now := s.now()
	signedSeed := int64(seed)
	att := store.Attempt{
		ID:         store.NewID(),
		UserID:     userID,
		LabID:      lab.Id,
		LabVersion: lab.Version,
		CaseID:     resolved.CaseID,
		Mode:       mode,
		ParamsJSON: string(paramsJSON),
		Seed:       &signedSeed,
		Status:     store.StatusProvisioning,
		CreatedAt:  now,
	}
	if err := s.store.Attempts.Create(ctx, att); err != nil {
		return View{}, err
	}

	s.track(att.ID)
	s.bus.publish(statusEvent(att, now))

	s.wg.Add(1)
	go s.provision(att.ID, lab, resolved, seed)

	return s.view(ctx, att)
}

func (s *Service) Get(ctx context.Context, id string) (View, error) {
	att, err := s.get(ctx, id)
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, att)
}

func (s *Service) Current(ctx context.Context, userID string) (View, bool, error) {
	att, ok, err := s.store.Attempts.ActiveForUser(ctx, userID)
	if err != nil || !ok {
		return View{}, false, err
	}
	view, err := s.view(ctx, att)
	if err != nil {
		return View{}, false, err
	}
	return view, true, nil
}

func (s *Service) Abandon(ctx context.Context, id string) (View, error) {
	att, err := s.end(ctx, id, store.StatusAbandoned, "", store.StatusProvisioning, store.StatusRunning)
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, att)
}

func (s *Service) Finish(ctx context.Context, id, status string) error {
	_, err := s.end(ctx, id, status, "", store.StatusRunning)
	return err
}

func (s *Service) Recover(ctx context.Context) error {
	attempts, err := s.store.Attempts.ListNonTerminal(ctx)
	if err != nil {
		return err
	}
	now := s.now()
	for _, att := range attempts {
		if err := s.store.Attempts.SetElapsed(ctx, att.ID, elapsedOf(att, now)); err != nil {
			return err
		}
		if err := s.store.Attempts.SetStatus(ctx, att.ID, store.StatusExpired, &now, ""); err != nil {
			return err
		}
	}
	s.log.Info("expired attempts left behind by the previous run", "count", len(attempts))

	if err := s.runner.GC(ctx); err != nil {
		return fmt.Errorf("collect leftover sandboxes: %w", err)
	}
	return nil
}

func (s *Service) Connected(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tracked[id]
	if !ok {
		return
	}
	t.conns++
	t.disarm()
}

func (s *Service) Disconnected(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tracked[id]
	if !ok {
		return
	}
	if t.conns > 0 {
		t.conns--
	}
	if t.conns == 0 {
		s.arm(id, t)
	}
}

func (s *Service) track(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tracked[id]; ok {
		return
	}
	t := &tracker{stop: make(chan struct{})}
	s.tracked[id] = t
	s.arm(id, t)
}

func (s *Service) untrack(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tracked[id]; ok {
		delete(s.tracked, id)
		t.close()
	}
}

func (s *Service) arm(id string, t *tracker) {
	if t.idle != nil {
		return
	}
	cancelled := make(chan struct{})
	t.idle = cancelled
	fired := s.after(s.idleTimeout)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		select {
		case <-fired:
			if s.armed(id, cancelled) {
				s.expire(id)
			}
		case <-cancelled:
		case <-s.ctx.Done():
		}
	}()
}

func (s *Service) armed(id string, idle chan struct{}) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tracked[id]
	return ok && t.idle == idle
}

func (s *Service) expire(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	if _, err := s.end(ctx, id, store.StatusExpired, "", store.StatusProvisioning, store.StatusRunning); err != nil {
		s.log.Error("expire idle attempt", "attempt", id, "error", err)
	}
}

func (s *Service) startTicks(id string, startedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tracked[id]
	if !ok {
		return
	}
	s.wg.Add(1)
	go s.tick(id, startedAt, t.stop)
}

func (s *Service) tick(id string, startedAt time.Time, stop <-chan struct{}) {
	defer s.wg.Done()
	for {
		select {
		case <-s.after(s.tickInterval):
			select {
			case <-stop:
				return
			default:
			}
			now := s.now()
			s.bus.publish(Event{
				AttemptID:  id,
				Type:       EventTick,
				ElapsedMS:  now.Sub(startedAt).Milliseconds(),
				ServerTime: now,
			})
		case <-stop:
			return
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *Service) provision(id string, lab content.Lab, resolved content.Resolved, seed uint64) {
	defer s.wg.Done()

	ctx, cancel := context.WithTimeout(s.ctx, time.Duration(provisionAttempts(lab))*provisionTimeout)
	defer cancel()

	sandbox, err := s.provisionUntilPrechecked(ctx, id, lab, resolved, seed)
	if err != nil {
		if s.stopping() {
			s.log.Warn("provisioning interrupted by shutdown", "attempt", id, "error", err)
			return
		}
		s.log.Error("provision attempt", "attempt", id, "error", err)
		s.failProvision(id, err)
		return
	}
	if err := s.promote(id, sandbox); err != nil {
		s.log.Error("start attempt after provisioning", "attempt", id, "error", err)
	}
}

func provisionAttempts(lab content.Lab) int {
	if lab.Precheck == nil {
		return 1
	}
	return lab.Precheck.Retries
}

func (s *Service) provisionUntilPrechecked(ctx context.Context, id string, lab content.Lab, resolved content.Resolved, seed uint64) (runner.SandboxID, error) {
	total := provisionAttempts(lab)
	for attempt := 1; ; attempt++ {
		sandbox, err := s.runProvision(ctx, id, lab, resolved)
		if err != nil {
			return "", err
		}
		if lab.Precheck == nil {
			return sandbox, nil
		}

		failure, err := s.runPrecheck(ctx, id, sandbox, lab, resolved)
		if err != nil {
			s.destroy(id, string(sandbox))
			return "", err
		}
		if failure == nil {
			return sandbox, nil
		}
		if attempt == total {
			s.destroy(id, string(sandbox))
			return "", fmt.Errorf("precheck failed after %d attempts on %s: %s", total, failure.node, failure.stderr)
		}

		s.bus.publish(Event{AttemptID: id, Type: EventProvisioning, Step: StepPrecheck, Attempt: attempt + 1, ServerTime: s.now()})
		s.destroy(id, string(sandbox))

		seed++
		resolved, err = lab.ResolveFor(seed)
		if err != nil {
			return "", fmt.Errorf("resolve params of lab %s: %w", lab.Id, err)
		}
		if err := s.storeResolved(id, seed, resolved); err != nil {
			return "", err
		}
	}
}

type precheckFailure struct {
	node   string
	stderr string
}

func (s *Service) runPrecheck(ctx context.Context, id string, sandbox runner.SandboxID, lab content.Lab, resolved content.Resolved) (*precheckFailure, error) {
	script, err := os.ReadFile(filepath.Join(lab.Dir, lab.Precheck.Script))
	if err != nil {
		return nil, fmt.Errorf("read precheck script: %w", err)
	}

	env := resolved.Env()
	for _, node := range slices.Sorted(maps.Keys(lab.Topology.Nodes)) {
		nodeEnv := maps.Clone(env)
		nodeEnv["NSL_NODE"] = node
		opts := runner.ExecOptions{Env: nodeEnv, Stdin: bytes.NewReader(script), Timeout: precheckTimeout}
		result, err := s.runner.Exec(ctx, sandbox, node, []string{"bash", "-s"}, opts)
		if err != nil {
			return nil, fmt.Errorf("run precheck on %s: %w", node, err)
		}
		if result.TimedOut {
			return &precheckFailure{node: node, stderr: fmt.Sprintf("timed out after %s", precheckTimeout)}, nil
		}
		if result.ExitCode != 0 {
			s.log.Info("precheck failed", "attempt", id, "node", node, "exit_code", result.ExitCode)
			return &precheckFailure{node: node, stderr: stderrTail(result.Stderr)}, nil
		}
	}
	return nil, nil
}

func (s *Service) storeResolved(id string, seed uint64, resolved content.Resolved) error {
	paramsJSON, err := json.Marshal(resolved.Params)
	if err != nil {
		return fmt.Errorf("encode params of attempt %s: %w", id, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	return s.store.Attempts.SetResolved(ctx, id, int64(seed), resolved.CaseID, string(paramsJSON))
}

func stderrTail(stderr []byte) string {
	lines := strings.Split(strings.TrimRight(string(stderr), "\n"), "\n")
	if len(lines) > precheckTailLines {
		lines = lines[len(lines)-precheckTailLines:]
	}
	return strings.Join(lines, "\n")
}

func newSeed() (uint64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, fmt.Errorf("draw an attempt seed: %w", err)
	}
	return binary.BigEndian.Uint64(b[:]), nil
}

func (s *Service) runProvision(ctx context.Context, id string, lab content.Lab, resolved content.Resolved) (runner.SandboxID, error) {
	var setup []byte
	if lab.Setup != "" {
		read, err := os.ReadFile(filepath.Join(lab.Dir, lab.Setup))
		if err != nil {
			return "", fmt.Errorf("read setup script: %w", err)
		}
		setup = read
	}

	spec, err := runner.SpecFromLab(id, s.image, lab, resolved, setup)
	if err != nil {
		return "", err
	}
	spec.Progress = func(step string) {
		s.bus.publish(Event{AttemptID: id, Type: EventProvisioning, Step: step, ServerTime: s.now()})
	}
	return s.runner.Provision(ctx, spec)
}

func (s *Service) failProvision(id string, cause error) {
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	if _, err := s.end(ctx, id, store.StatusError, cause.Error(), store.StatusProvisioning); err != nil {
		s.log.Error("record failed provisioning", "attempt", id, "error", err)
	}
}

func (s *Service) promote(id string, sandbox runner.SandboxID) error {
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()

	att, err := s.get(ctx, id)
	if err != nil {
		return err
	}
	if att.Status != store.StatusProvisioning {
		s.destroy(id, string(sandbox))
		return nil
	}

	now := s.now()
	if err := s.store.Attempts.SetSandbox(ctx, id, s.runnerID, string(sandbox)); err != nil {
		return err
	}
	if err := s.store.Attempts.SetStartedAt(ctx, id, now); err != nil {
		return err
	}
	if err := s.store.Attempts.SetStatus(ctx, id, store.StatusRunning, nil, ""); err != nil {
		return err
	}

	att.Status = store.StatusRunning
	att.StartedAt = &now
	att.SandboxID = string(sandbox)
	s.bus.publish(statusEvent(att, now))
	s.startTicks(id, now)
	return nil
}

func (s *Service) end(ctx context.Context, id, status, errorMessage string, from ...string) (store.Attempt, error) {
	att, err := s.get(ctx, id)
	if err != nil {
		return store.Attempt{}, err
	}
	if isTerminal(att.Status) {
		return store.Attempt{}, fmt.Errorf("%w: %s is %s", ErrTerminal, id, att.Status)
	}
	if !slices.Contains(from, att.Status) {
		return store.Attempt{}, fmt.Errorf("attempt %s is %s, expected %s", id, att.Status, strings.Join(from, " or "))
	}

	now := s.now()
	elapsed := elapsedOf(att, now)
	if err := s.store.Attempts.SetElapsed(ctx, id, elapsed); err != nil {
		return store.Attempt{}, err
	}
	if err := s.store.Attempts.SetStatus(ctx, id, status, &now, errorMessage); err != nil {
		return store.Attempt{}, err
	}

	att.Status = status
	att.ErrorMessage = errorMessage
	att.EndedAt = &now
	att.ElapsedMS = elapsed.Milliseconds()

	if status == store.StatusPassed {
		if err := s.store.Progress.Upsert(ctx, att.UserID, store.ProgressLab, att.LabID); err != nil {
			return store.Attempt{}, err
		}
	}

	s.untrack(id)
	s.runBeforeDestroy(ctx, att)
	s.bus.publish(statusEvent(att, now))
	s.destroyAsync(att.ID, att.SandboxID)
	return att, nil
}

func (s *Service) runBeforeDestroy(ctx context.Context, att store.Attempt) {
	s.hooksMu.Lock()
	hooks := slices.Clone(s.hooks)
	s.hooksMu.Unlock()
	if len(hooks) == 0 {
		return
	}

	hookCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.hookTimeout)
	defer cancel()

	view, err := s.view(hookCtx, att)
	if err != nil {
		s.log.Error("build the view for the before-destroy hooks", "attempt", att.ID, "error", err)
		return
	}

	for _, hook := range hooks {
		s.runHook(hookCtx, hook, view)
	}
}

func (s *Service) runHook(ctx context.Context, hook BeforeDestroyFunc, view View) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("before-destroy hook panicked", "attempt", view.Id, "panic", r)
			}
		}()
		hook(ctx, view)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		s.log.Warn("before-destroy hook did not finish before the deadline", "attempt", view.Id)
	}
}

func (s *Service) destroyAsync(id, sandbox string) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.destroy(id, sandbox)
	}()
}

func (s *Service) destroy(id, sandbox string) {
	if sandbox == "" {
		sandbox = id
	}
	ctx, cancel := context.WithTimeout(context.Background(), destroyTimeout)
	defer cancel()
	if err := s.runner.Destroy(ctx, runner.SandboxID(sandbox)); err != nil && !errors.Is(err, runner.ErrSandboxNotFound) {
		s.log.Error("destroy sandbox", "attempt", id, "sandbox", sandbox, "error", err)
	}
}

func (s *Service) get(ctx context.Context, id string) (store.Attempt, error) {
	att, err := s.store.Attempts.Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return store.Attempt{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return store.Attempt{}, err
	}
	return att, nil
}

func (s *Service) view(ctx context.Context, att store.Attempt) (View, error) {
	lab, ok := s.labs[att.LabID]
	if !ok {
		return View{}, fmt.Errorf("%w: %s", ErrUnknownLab, att.LabID)
	}
	var params map[string]string
	if err := json.Unmarshal([]byte(att.ParamsJSON), &params); err != nil {
		return View{}, fmt.Errorf("decode params of attempt %s: %w", att.ID, err)
	}
	resolved := content.Resolved{CaseID: att.CaseID, Params: params}
	if picked, ok := lab.CaseByID(att.CaseID); ok {
		resolved.SetupEnv = picked.SetupEnv
	}
	runs, err := s.store.CheckpointRuns.ListByAttempt(ctx, att.ID)
	if err != nil {
		return View{}, err
	}
	return newView(att, lab, resolved, runs, s.now())
}

func (s *Service) stopping() bool {
	select {
	case <-s.ctx.Done():
		return true
	default:
		return false
	}
}

func statusEvent(att store.Attempt, now time.Time) Event {
	return Event{
		AttemptID:    att.ID,
		Type:         EventStatus,
		Status:       att.Status,
		ErrorMessage: att.ErrorMessage,
		ElapsedMS:    elapsedMS(att, now),
		ServerTime:   now,
	}
}

func isTerminal(status string) bool {
	switch status {
	case store.StatusProvisioning, store.StatusRunning:
		return false
	default:
		return true
	}
}
