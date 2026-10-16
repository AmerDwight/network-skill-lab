package runnerclient

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/runnerproto"
	"github.com/AmerDwight/network-skill-lab/internal/runnerserver"
	"github.com/AmerDwight/network-skill-lab/internal/store"
	"github.com/coder/websocket"
)

const (
	runnerName  = "lab-01"
	waitTimeout = 5 * time.Second
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type harness struct {
	t        *testing.T
	store    *store.Store
	hub      *runnerserver.Hub
	server   *httptest.Server
	url      string
	token    string
	provider *fakeProvider

	mu    sync.Mutex
	conns []net.Conn
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	hub := runnerserver.NewHub(st.Runners, quiet)
	server := httptest.NewServer(hub)
	t.Cleanup(server.Close)

	_, token, err := runnerserver.Register(t.Context(), st.Runners, runnerName, nil, 3)
	if err != nil {
		t.Fatalf("register runner: %v", err)
	}
	return &harness{
		t:        t,
		store:    st,
		hub:      hub,
		server:   server,
		url:      "ws" + strings.TrimPrefix(server.URL, "http"),
		token:    token,
		provider: newFakeProvider(),
	}
}

func (h *harness) newClient(token string) *client {
	c := newClient(Options{
		URL:      h.url,
		Token:    token,
		Name:     runnerName,
		Capacity: 3,
		Version:  "test",
		Provider: h.provider,
		Logger:   quiet,
	})
	c.pingInterval = 20 * time.Millisecond
	c.minBackoff = 10 * time.Millisecond
	c.maxBackoff = 50 * time.Millisecond
	c.httpClient = &http.Client{Transport: &http.Transport{DialContext: h.dial}}
	return c
}

func (h *harness) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, network, addr)
	if err == nil {
		h.mu.Lock()
		h.conns = append(h.conns, conn)
		h.mu.Unlock()
	}
	return conn, err
}

func (h *harness) killConnections() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, conn := range h.conns {
		_ = conn.Close()
	}
	h.conns = nil
}

func (h *harness) start(c *client) <-chan error {
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		result <- c.run(ctx)
	}()
	h.t.Cleanup(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(waitTimeout):
			h.t.Error("runner client did not stop")
		}
	})
	return result
}

func (h *harness) connect() (*client, runner.Runner) {
	h.t.Helper()
	c := h.newClient(h.token)
	h.start(c)
	return c, h.waitRunner()
}

func (h *harness) waitRunner() runner.Runner {
	h.t.Helper()
	var remote runner.Runner
	eventually(h.t, "runner connects", func() bool {
		var ok bool
		remote, ok = h.hub.Get(runnerName)
		return ok
	})
	return remote
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting until %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func provision(t *testing.T, remote runner.Runner, attempt string) (runner.SandboxID, []string) {
	t.Helper()
	var steps []string
	spec := runner.SandboxSpec{
		AttemptID: attempt,
		Image:     "nsl/node",
		Nodes:     []runner.NodeSpec{{Name: "web01", Role: "ubuntu"}},
		Env:       map[string]string{"NSL_X": "1"},
		Progress:  func(step string) { steps = append(steps, step) },
	}
	sb, err := remote.Provision(t.Context(), spec)
	if err != nil {
		t.Fatalf("Provision() error = %v", err)
	}
	return sb, steps
}

func readUntil(t *testing.T, r io.Reader, want string) string {
	t.Helper()
	var got strings.Builder
	buf := make([]byte, 1024)
	deadline := time.Now().Add(waitTimeout)
	for !strings.Contains(got.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("read %q, want it to contain %q", got.String(), want)
		}
		n, err := r.Read(buf)
		got.Write(buf[:n])
		if err != nil {
			t.Fatalf("read %q then error %v, want %q", got.String(), err, want)
		}
	}
	return got.String()
}

func TestRemoteRunnerRoundTrip(t *testing.T) {
	h := newHarness(t)
	_, remote := h.connect()
	ctx := t.Context()

	if got := remote.Capabilities(); !slices.Equal(got, []runner.Env{runner.EnvContainer}) {
		t.Errorf("Capabilities() = %v", got)
	}

	sb, steps := provision(t, remote, "att1")
	if sb != "att1" {
		t.Errorf("Provision() = %q", sb)
	}
	if !slices.Equal(steps, []string{"network", "nodes", "setup"}) {
		t.Errorf("progress steps = %v", steps)
	}

	result, err := remote.Exec(ctx, sb, "web01", []string{"bash", "-s"}, runner.ExecOptions{
		Stdin: strings.NewReader("echo hi\x00"),
		Env:   map[string]string{"NSL_X": "x"},
	})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if string(result.Stdout) != "web01|bash -s|echo hi\x00|x" || string(result.Stderr) != "warn\xff" || result.ExitCode != 3 {
		t.Errorf("Exec() = %+v", result)
	}
	if _, err := remote.Exec(ctx, "missing", "web01", []string{"true"}, runner.ExecOptions{}); !errors.Is(err, runner.ErrSandboxNotFound) {
		t.Errorf("Exec(missing sandbox) error = %v, want ErrSandboxNotFound", err)
	}

	pty, err := remote.OpenTerminal(ctx, sb, "web01")
	if err != nil {
		t.Fatalf("OpenTerminal() error = %v", err)
	}
	if _, err := pty.Write([]byte("ls\r")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	readUntil(t, pty, "echo:ls\r")
	if err := pty.Resize(120, 40); err != nil {
		t.Fatalf("Resize() error = %v", err)
	}
	local := h.provider.pty(0)
	if got := local.dims(); got != [2]uint16{120, 40} {
		t.Errorf("runner side size = %v", got)
	}
	if err := pty.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if n, err := pty.Read(make([]byte, 8)); n != 0 || !errors.Is(err, io.EOF) {
		t.Errorf("Read() after Close = %d, %v, want EOF", n, err)
	}
	eventually(t, "the runner closes its pty", local.closed)

	second, err := remote.OpenTerminal(ctx, sb, "web01")
	if err != nil {
		t.Fatalf("OpenTerminal() error = %v", err)
	}
	h.provider.pty(1).exit()
	if _, err := io.ReadAll(second); err != nil {
		t.Errorf("Read() after the shell exited error = %v, want EOF", err)
	}
	_ = second.Close()
	if _, err := remote.OpenTerminal(ctx, "missing", "web01"); !errors.Is(err, runner.ErrSandboxNotFound) {
		t.Errorf("OpenTerminal(missing) error = %v", err)
	}

	health := remote.Health(ctx)
	if !health.OK || health.Instance != "fake" || health.MemAvailableMB != 1024 {
		t.Errorf("Health() = %+v", health)
	}

	eventually(t, "the hub sees one active sandbox", func() bool {
		infos := h.hub.Runners()
		return len(infos) == 1 && infos[0].Active == 1
	})
	info := h.hub.Runners()[0]
	if info.Name != runnerName || !info.Connected || info.Capacity != 3 || info.Version != "test" || info.LastSeen.IsZero() {
		t.Errorf("Runners() = %+v", info)
	}
	record, err := h.store.Runners.ByName(ctx, runnerName)
	if err != nil {
		t.Fatal(err)
	}
	if record.LastSeenAt == nil || record.Version != "test" {
		t.Errorf("stored runner = %+v", record)
	}

	if err := remote.Destroy(ctx, sb); err != nil {
		t.Fatalf("Destroy() error = %v", err)
	}
	if h.provider.has(sb) {
		t.Error("sandbox survived Destroy")
	}
	if err := remote.GC(ctx); err != nil {
		t.Fatalf("GC() error = %v", err)
	}
	eventually(t, "the hub sees no active sandbox", func() bool { return h.hub.Runners()[0].Active == 0 })
}

func TestCallCancellation(t *testing.T) {
	h := newHarness(t)
	h.provider.block = make(chan struct{})
	_, remote := h.connect()
	sb, _ := provision(t, remote, "att1")

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := remote.Exec(ctx, sb, "web01", []string{"sleep", "60"}, runner.ExecOptions{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Exec() error = %v, want DeadlineExceeded", err)
	}
	select {
	case <-h.provider.canceled:
	case <-time.After(waitTimeout):
		t.Fatal("the runner did not cancel the exec")
	}
}

func TestAuthFailures(t *testing.T) {
	h := newHarness(t)

	if err := <-h.start(h.newClient("wrong-token")); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("bad token: Run() = %v, want ErrUnauthorized", err)
	}

	c := h.newClient(h.token)
	c.opts.Name = "nobody"
	if err := <-h.start(c); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("unknown runner: Run() = %v, want ErrUnauthorized", err)
	}

	record, err := h.store.Runners.ByName(t.Context(), runnerName)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.Runners.Revoke(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	if err := <-h.start(h.newClient(h.token)); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("revoked runner: Run() = %v, want ErrUnauthorized", err)
	}
	if len(h.hub.Runners()) != 0 {
		t.Errorf("rejected runners were registered: %+v", h.hub.Runners())
	}
}

func TestProtocolMismatch(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	ws, _, err := websocket.Dial(ctx, h.url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + h.token}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ws.CloseNow() }()
	data, _ := runnerproto.Encode(runnerproto.Hello{Proto: runnerproto.Version + 1, Name: runnerName})
	if err := ws.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
	_, _, err = ws.Read(ctx)
	if got := websocket.CloseStatus(err); got != runnerproto.CloseProtocolMismatch {
		t.Fatalf("close status = %d (%v), want %d", got, err, runnerproto.CloseProtocolMismatch)
	}
}

func TestRevokeKicksRunner(t *testing.T) {
	h := newHarness(t)
	c := h.newClient(h.token)
	done := h.start(c)
	h.waitRunner()

	h.hub.Disconnect(runnerName)
	select {
	case err := <-done:
		if !errors.Is(err, ErrUnauthorized) {
			t.Errorf("Run() = %v, want ErrUnauthorized", err)
		}
	case <-time.After(waitTimeout):
		t.Fatal("the runner kept running after it was kicked")
	}
}

func TestDisconnectAndReconnect(t *testing.T) {
	h := newHarness(t)
	disconnects := make(chan string, 4)
	reconnects := make(chan []string, 4)
	h.hub.OnDisconnect(func(name string) { disconnects <- name })
	h.hub.OnReconnect(func(name string, sandboxes []string) {
		if remote, ok := h.hub.Get(name); ok {
			for _, sb := range sandboxes {
				_ = remote.Destroy(context.Background(), runner.SandboxID(sb))
			}
		}
		reconnects <- sandboxes
	})
	h.provider.block = make(chan struct{})

	c, remote := h.connect()
	sb, _ := provision(t, remote, "att1")

	inflight := make(chan error, 1)
	go func() {
		_, err := remote.Exec(context.Background(), sb, "web01", []string{"sleep", "60"}, runner.ExecOptions{})
		inflight <- err
	}()
	time.Sleep(20 * time.Millisecond)

	h.killConnections()

	select {
	case name := <-disconnects:
		if name != runnerName {
			t.Errorf("OnDisconnect(%q)", name)
		}
	case <-time.After(waitTimeout):
		t.Fatal("OnDisconnect did not fire")
	}
	select {
	case err := <-inflight:
		if !errors.Is(err, runnerserver.ErrDisconnected) {
			t.Errorf("in-flight Exec() error = %v, want ErrDisconnected", err)
		}
	case <-time.After(waitTimeout):
		t.Fatal("the in-flight call did not return")
	}

	select {
	case sandboxes := <-reconnects:
		if !slices.Equal(sandboxes, []string{string(sb)}) {
			t.Errorf("OnReconnect sandboxes = %v", sandboxes)
		}
	case <-time.After(waitTimeout):
		t.Fatal("OnReconnect did not fire")
	}
	if h.provider.has(sb) {
		t.Error("the reported sandbox was not destroyed")
	}
	eventually(t, "the runner forgets the destroyed sandbox", func() bool { return c.active() == 0 })
	infos := h.hub.Runners()
	if len(infos) != 1 || !infos[0].Connected {
		t.Errorf("Runners() after reconnect = %+v", infos)
	}
}

func TestRetentionDestroysSandboxes(t *testing.T) {
	h := newHarness(t)
	c := h.newClient(h.token)
	c.retention = 100 * time.Millisecond
	h.start(c)
	remote := h.waitRunner()
	sb, _ := provision(t, remote, "att1")

	_ = h.server.Listener.Close()
	h.killConnections()

	eventually(t, "the runner destroys the sandbox it kept", func() bool {
		return slices.Contains(h.provider.destroyedList(), sb)
	})
	if c.active() != 0 {
		t.Errorf("active = %d after retention", c.active())
	}
}

func TestRetentionKeepsSandboxesAcrossAQuickReconnect(t *testing.T) {
	h := newHarness(t)
	c := h.newClient(h.token)
	c.retention = time.Hour
	h.start(c)
	remote := h.waitRunner()
	sb, _ := provision(t, remote, "att1")

	reconnects := make(chan []string, 1)
	h.hub.OnReconnect(func(_ string, sandboxes []string) { reconnects <- sandboxes })
	h.killConnections()

	select {
	case sandboxes := <-reconnects:
		if !slices.Equal(sandboxes, []string{string(sb)}) {
			t.Errorf("OnReconnect sandboxes = %v", sandboxes)
		}
	case <-time.After(waitTimeout):
		t.Fatal("OnReconnect did not fire")
	}
	if !h.provider.has(sb) || len(h.provider.destroyedList()) != 0 {
		t.Error("the runner destroyed a sandbox before the control plane asked")
	}
}

func TestStopDestroysSandboxes(t *testing.T) {
	h := newHarness(t)
	c := h.newClient(h.token)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.run(ctx) }()
	remote := h.waitRunner()
	sb, _ := provision(t, remote, "att1")

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run() after cancel = %v, want nil", err)
	}
	if !slices.Contains(h.provider.destroyedList(), sb) {
		t.Error("stopping the runner left its sandbox behind")
	}
}
