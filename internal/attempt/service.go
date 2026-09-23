package attempt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	destroyTimeout      = 2 * time.Minute
	storeTimeout        = 10 * time.Second
)

var (
	ErrActiveAttempt  = errors.New("user already has an active attempt")
	ErrNotFound       = errors.New("attempt not found")
	ErrTerminal       = errors.New("attempt has already ended")
	ErrNotTerminal    = errors.New("attempt has not ended yet")
	ErrUnknownLab     = errors.New("unknown lab")
	ErrModeNotAllowed = errors.New("mode not allowed by the lab")
)

type Deps struct {
	Store        *store.Store
	Runner       runner.Runner
	Labs         []content.Lab
	Image        string
	RunnerID     string
	IdleTimeout  time.Duration
	TickInterval time.Duration
	Now          func() time.Time
	After        func(time.Duration) <-chan time.Time
	Logger       *slog.Logger
}

type Service struct {
	store        *store.Store
	runner       runner.Runner
	labs         map[string]content.Lab
	image        string
	runnerID     string
	idleTimeout  time.Duration
	tickInterval time.Duration
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
}

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
	labs := make(map[string]content.Lab, len(deps.Labs))
	for _, lab := range deps.Labs {
		labs[lab.Id] = lab
	}
	if deps.IdleTimeout <= 0 {
		deps.IdleTimeout = defaultIdleTimeout
	}
	if deps.TickInterval <= 0 {
		deps.TickInterval = defaultTickInterval
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
		image:        deps.Image,
		runnerID:     deps.RunnerID,
		idleTimeout:  deps.IdleTimeout,
		tickInterval: deps.TickInterval,
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

	params, err := lab.Params.Resolve()
	if err != nil {
		return View{}, fmt.Errorf("resolve params of lab %s: %w", labID, err)
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return View{}, fmt.Errorf("encode params of lab %s: %w", labID, err)
	}

	now := s.now()
	att := store.Attempt{
		ID:         store.NewID(),
		UserID:     userID,
		LabID:      lab.Id,
		LabVersion: lab.Version,
		Mode:       mode,
		ParamsJSON: string(paramsJSON),
		Status:     store.StatusProvisioning,
		CreatedAt:  now,
	}
	if err := s.store.Attempts.Create(ctx, att); err != nil {
		return View{}, err
	}

	s.track(att.ID)
	s.bus.publish(statusEvent(att, now))

	s.wg.Add(1)
	go s.provision(att.ID, lab, params)

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

func (s *Service) provision(id string, lab content.Lab, params map[string]string) {
	defer s.wg.Done()

	ctx, cancel := context.WithTimeout(s.ctx, provisionTimeout)
	defer cancel()

	sandbox, err := s.runProvision(ctx, id, lab, params)
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

func (s *Service) runProvision(ctx context.Context, id string, lab content.Lab, params map[string]string) (runner.SandboxID, error) {
	var setup []byte
	if lab.Setup != "" {
		read, err := os.ReadFile(filepath.Join(lab.Dir, lab.Setup))
		if err != nil {
			return "", fmt.Errorf("read setup script: %w", err)
		}
		setup = read
	}

	spec, err := runner.SpecFromLab(id, s.image, lab, params, setup)
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

	s.untrack(id)
	s.bus.publish(statusEvent(att, now))
	s.destroyAsync(att.ID, att.SandboxID)
	return att, nil
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
	runs, err := s.store.CheckpointRuns.ListByAttempt(ctx, att.ID)
	if err != nil {
		return View{}, err
	}
	return newView(att, lab, params, runs, s.now())
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
