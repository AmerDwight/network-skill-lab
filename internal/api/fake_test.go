package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/attempt"
	"github.com/AmerDwight/network-skill-lab/internal/auth"
	"github.com/AmerDwight/network-skill-lab/internal/checker"
	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/content/contenttest"
	"github.com/AmerDwight/network-skill-lab/internal/recorder"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

const waitTimeout = 5 * time.Second

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

type waiter struct {
	d  time.Duration
	ch chan time.Time
}

type fakeClock struct {
	mu      sync.Mutex
	waiters []waiter
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.waiters = append(c.waiters, waiter{d: d, ch: ch})
	return ch
}

func (c *fakeClock) pending(d time.Duration) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, w := range c.waiters {
		if w.d == d {
			n++
		}
	}
	return n
}

func (c *fakeClock) fire(t *testing.T, d time.Duration) {
	t.Helper()
	waitUntil(t, "a timer armed for "+d.String(), func() bool { return c.pending(d) > 0 })

	c.mu.Lock()
	defer c.mu.Unlock()
	var kept, fired []waiter
	for _, w := range c.waiters {
		if w.d == d {
			fired = append(fired, w)
			continue
		}
		kept = append(kept, w)
	}
	c.waiters = kept
	for _, w := range fired {
		w.ch <- time.Now()
	}
}

type fakePTY struct {
	reader *io.PipeReader
	writer *io.PipeWriter

	mu     sync.Mutex
	input  bytes.Buffer
	cols   uint16
	rows   uint16
	closed bool
}

func newFakePTY() *fakePTY {
	reader, writer := io.Pipe()
	return &fakePTY{reader: reader, writer: writer}
}

func (p *fakePTY) Read(b []byte) (int, error) {
	return p.reader.Read(b)
}

func (p *fakePTY) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return 0, io.ErrClosedPipe
	}
	return p.input.Write(b)
}

func (p *fakePTY) Resize(cols, rows uint16) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return io.ErrClosedPipe
	}
	p.cols, p.rows = cols, rows
	return nil
}

func (p *fakePTY) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()

	_ = p.writer.Close()
	return p.reader.Close()
}

func (p *fakePTY) emit(data []byte) error {
	_, err := p.writer.Write(data)
	return err
}

func (p *fakePTY) eof() {
	_ = p.writer.Close()
}

func (p *fakePTY) received() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.input.String()
}

func (p *fakePTY) size() (uint16, uint16) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cols, p.rows
}

func (p *fakePTY) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

type fakeRunner struct {
	mu        sync.Mutex
	ptys      []*fakePTY
	openErr   error
	health    runner.Health
	steps     []string
	checkExit int
	gate      chan struct{}
	opening   chan struct{}
}

var _ runner.Runner = (*fakeRunner)(nil)

func newFakeRunner() *fakeRunner {
	return &fakeRunner{
		health:    runner.Health{OK: true, Docker: true, Image: true, ImageName: "nsl/node", MemAvailableMB: 2048},
		steps:     []string{"networks", "containers", "bootstrap", "setup"},
		checkExit: 1,
	}
}

func (f *fakeRunner) Capabilities() []runner.Env {
	return []runner.Env{runner.EnvContainer}
}

func (f *fakeRunner) hold() func() {
	gate := make(chan struct{})
	f.mu.Lock()
	f.gate = gate
	f.mu.Unlock()
	return func() { close(gate) }
}

func (f *fakeRunner) Provision(ctx context.Context, spec runner.SandboxSpec) (runner.SandboxID, error) {
	f.mu.Lock()
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	for _, step := range f.steps {
		if spec.Progress != nil {
			spec.Progress(step)
		}
	}
	return runner.SandboxID(spec.AttemptID), nil
}

func (f *fakeRunner) holdTerminals() func() {
	opening := make(chan struct{})
	f.mu.Lock()
	f.opening = opening
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		f.opening = nil
		f.mu.Unlock()
		close(opening)
	}
}

func (f *fakeRunner) OpenTerminal(_ context.Context, _ runner.SandboxID, _ string) (runner.PTY, error) {
	f.mu.Lock()
	if f.openErr != nil {
		f.mu.Unlock()
		return nil, f.openErr
	}
	pty := newFakePTY()
	f.ptys = append(f.ptys, pty)
	opening := f.opening
	f.mu.Unlock()

	if opening != nil {
		<-opening
	}
	return pty, nil
}

func (f *fakeRunner) Exec(_ context.Context, _ runner.SandboxID, _ string, _ []string, opts runner.ExecOptions) (runner.ExecResult, error) {
	if opts.Stdin != nil {
		if _, err := io.Copy(io.Discard, opts.Stdin); err != nil {
			return runner.ExecResult{}, err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return runner.ExecResult{ExitCode: f.checkExit}, nil
}

func (f *fakeRunner) passChecks() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checkExit = 0
}

func (f *fakeRunner) Destroy(_ context.Context, _ runner.SandboxID) error { return nil }

func (f *fakeRunner) GC(_ context.Context) error { return nil }

func (f *fakeRunner) Health(_ context.Context) runner.Health {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.health
}

func (f *fakeRunner) terminal(t *testing.T, index int) *fakePTY {
	t.Helper()
	waitUntil(t, "terminal to be opened", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.ptys) > index
	})
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ptys[index]
}

type harness struct {
	t        *testing.T
	server   *httptest.Server
	attempts *attempt.Service
	checker  *checker.Checker
	store    *store.Store
	runner   *fakeRunner
	clock    *fakeClock
	auth     *auth.Service
	recorder *recorder.Recorder
	labs     []content.Lab
	user     store.User
	admin    store.User
	other    store.User
	dataDir  string
	client   *http.Client
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessWithContent(t, contenttest.Dir())
}

func newHarnessWithContent(t *testing.T, dir string) *harness {
	t.Helper()
	return newHarnessWith(t, dir, 0)
}

func newHarnessWith(t *testing.T, dir string, maxSandboxes int) *harness {
	t.Helper()
	h := newBareHarness(t, dir, maxSandboxes)
	h.user = h.addUser(testUsername, testPassword, store.RoleUser)
	h.admin = h.addUser("root", testPassword, store.RoleAdmin)
	h.other = h.addUser("mallory", testPassword, store.RoleUser)
	h.client = h.login(testUsername, testPassword)
	return h
}

func newBareHarness(t *testing.T, dir string, maxSandboxes int) *harness {
	t.Helper()

	loaded, err := content.LoadAll(dir)
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	fake := newFakeRunner()
	clock := &fakeClock{}
	logger := discardLogger()
	attempts := attempt.New(attempt.Deps{
		Store:        st,
		Runner:       fake,
		Content:      loaded,
		Image:        "nsl/node",
		RunnerID:     "fake",
		DataDir:      dataDir,
		MaxSandboxes: maxSandboxes,
		After:        clock.After,
		Logger:       logger,
	})
	t.Cleanup(attempts.Close)

	checks := checker.New(checker.Deps{
		Store:    st,
		Runner:   fake,
		Attempts: attempts,
		Interval: time.Hour,
		Logger:   logger,
	})
	t.Cleanup(checks.Close)
	attempts.SetSweeper(checks)
	checks.Start(t.Context())

	recordings := recorder.New(recorder.Deps{
		Store:    st,
		Runner:   fake,
		Attempts: attempts,
		DataDir:  dataDir,
		Logger:   logger,
	})
	t.Cleanup(recordings.Close)

	sessions := auth.New(st)
	server := httptest.NewServer(New(Deps{
		Attempts:     attempts,
		Content:      loaded,
		Store:        st,
		Runner:       fake,
		Recorder:     recordings,
		Auth:         sessions,
		MaxSandboxes: maxSandboxes,
		Logger:       logger,
	}))
	t.Cleanup(server.Close)

	return &harness{
		t:        t,
		server:   server,
		attempts: attempts,
		checker:  checks,
		store:    st,
		runner:   fake,
		clock:    clock,
		auth:     sessions,
		recorder: recordings,
		labs:     loaded.Labs,
		dataDir:  dataDir,
	}
}

const (
	testUsername = "tester"
	testPassword = "correct horse"
)

func (h *harness) addUser(username, password, role string) store.User {
	h.t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		h.t.Fatalf("hash password: %v", err)
	}
	user := store.User{ID: store.NewID(), Username: username, PasswordHash: hash, Role: role}
	if err := h.store.Users.Create(h.t.Context(), user); err != nil {
		h.t.Fatalf("create user %s: %v", username, err)
	}
	created, err := h.store.Users.ByID(h.t.Context(), user.ID)
	if err != nil {
		h.t.Fatalf("read back user %s: %v", username, err)
	}
	return created
}

func (h *harness) login(username, password string) *http.Client {
	h.t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		h.t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar, Transport: h.server.Client().Transport}
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	resp := h.send(client, http.MethodPost, "/api/auth/login", body)
	if resp.StatusCode != http.StatusOK {
		h.t.Fatalf("login as %s: status = %d", username, resp.StatusCode)
	}
	return client
}

func (h *harness) do(method, path, body string) *http.Response {
	h.t.Helper()
	return h.send(h.client, method, path, body)
}

func (h *harness) request(method, path, body string) *http.Request {
	h.t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(h.t.Context(), method, h.server.URL+path, reader)
	if err != nil {
		h.t.Fatalf("build request %s %s: %v", method, path, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func (h *harness) send(client *http.Client, method, path, body string) *http.Response {
	h.t.Helper()
	resp, err := client.Do(h.request(method, path, body))
	if err != nil {
		h.t.Fatalf("send request %s %s: %v", method, path, err)
	}
	h.t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func (h *harness) start() string {
	h.t.Helper()
	return h.startMode("guided")
}

func (h *harness) startMode(mode string) string {
	h.t.Helper()
	return h.startLab(h.labs[0].Id, mode)
}

func (h *harness) startLab(labID, mode string) string {
	h.t.Helper()
	resp := h.do(http.MethodPost, "/api/attempts", `{"lab_id":"`+labID+`","mode":"`+mode+`"}`)
	if resp.StatusCode != http.StatusCreated {
		h.t.Fatalf("start attempt: status = %d", resp.StatusCode)
	}
	return decodeJSON(h.t, resp)["id"].(string)
}

func (h *harness) running() string {
	h.t.Helper()
	return h.runningMode("guided")
}

func (h *harness) runningMode(mode string) string {
	h.t.Helper()
	return h.runningLab(h.labs[0].Id, mode)
}

func (h *harness) runningLab(labID, mode string) string {
	h.t.Helper()
	id := h.startLab(labID, mode)
	waitUntil(h.t, "the attempt to be running", func() bool {
		view, err := h.attempts.Get(h.t.Context(), id)
		return err == nil && view.Status == store.StatusRunning
	})
	return id
}

func (h *harness) submit(id string) map[string]any {
	h.t.Helper()
	resp := h.do(http.MethodPost, "/api/attempts/"+id+"/submit", "")
	if resp.StatusCode != http.StatusOK {
		h.t.Fatalf("submit attempt: status = %d", resp.StatusCode)
	}
	return decodeJSON(h.t, resp)
}

func (h *harness) writeCast(id, node, tab string) store.Recording {
	h.t.Helper()
	cast, err := h.recorder.OpenRecording(h.t.Context(), id, node, tab, 80, 24)
	if err != nil {
		h.t.Fatalf("open recording: %v", err)
	}
	if err := cast.Output([]byte("hello from " + node + "\r\n")); err != nil {
		h.t.Fatalf("write recording: %v", err)
	}
	if err := cast.Close(); err != nil {
		h.t.Fatalf("close recording: %v", err)
	}

	found, err := h.store.Recordings.ListByAttempt(h.t.Context(), id)
	if err != nil {
		h.t.Fatalf("list recordings: %v", err)
	}
	for _, rec := range found {
		if rec.Node == node && rec.TabID == tab {
			return rec
		}
	}
	h.t.Fatalf("recording for %s/%s was not stored", node, tab)
	return store.Recording{}
}

func (h *harness) socketURL(path string) string {
	return "ws" + strings.TrimPrefix(h.server.URL, "http") + path
}
