package runnerserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/runnerproto"
	"github.com/AmerDwight/network-skill-lab/internal/store"
	"github.com/coder/websocket"
)

const (
	testRunner  = "lab-01"
	waitTimeout = 5 * time.Second
)

type fixture struct {
	t     *testing.T
	hub   *Hub
	url   string
	token string
	store *store.Store
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	hub := NewHub(st.Runners, slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(hub)
	t.Cleanup(server.Close)
	_, token, err := Register(t.Context(), st.Runners, testRunner, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, hub: hub, url: "ws" + strings.TrimPrefix(server.URL, "http"), token: token, store: st}
}

func (f *fixture) dial(token string) *websocket.Conn {
	f.t.Helper()
	header := http.Header{}
	if token != "" {
		header.Set("Authorization", "Bearer "+token)
	}
	ws, _, err := websocket.Dial(f.t.Context(), f.url, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		f.t.Fatal(err)
	}
	ws.SetReadLimit(readLimit)
	f.t.Cleanup(func() { _ = ws.CloseNow() })
	return ws
}

func (f *fixture) handshake(hello runnerproto.Hello) *websocket.Conn {
	f.t.Helper()
	ws := f.dial(f.token)
	writeMessage(f.t, ws, hello)
	if _, ok := readMessage(f.t, ws).(runnerproto.Welcome); !ok {
		f.t.Fatal("no welcome")
	}
	return ws
}

func defaultHello() runnerproto.Hello {
	return runnerproto.Hello{Proto: runnerproto.Version, Name: testRunner, Version: "test", Capabilities: []string{"container"}, Capacity: 2}
}

func writeMessage(t *testing.T, ws *websocket.Conn, m runnerproto.Message) {
	t.Helper()
	data, err := runnerproto.Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.Write(t.Context(), websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
}

func readMessage(t *testing.T, ws *websocket.Conn) runnerproto.Message {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	defer cancel()
	kind, data, err := ws.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if kind != websocket.MessageText {
		t.Fatalf("got a %v message", kind)
	}
	m, err := runnerproto.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func waitFor[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(waitTimeout):
		t.Fatalf("timed out waiting for %s", what)
		var zero T
		return zero
	}
}

func remote(t *testing.T, hub *Hub) runner.Runner {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for {
		if r, ok := hub.Get(testRunner); ok {
			return r
		}
		if time.Now().After(deadline) {
			t.Fatal("runner did not register")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestMissingTokenIsUnauthorized(t *testing.T) {
	f := newFixture(t)
	ws := f.dial("")
	writeMessage(t, ws, defaultHello())
	_, _, err := ws.Read(t.Context())
	if got := websocket.CloseStatus(err); got != runnerproto.CloseUnauthorized {
		t.Fatalf("close status = %d (%v), want %d", got, err, runnerproto.CloseUnauthorized)
	}
}

func TestNonHelloIsAProtocolError(t *testing.T) {
	f := newFixture(t)
	ws := f.dial(f.token)
	writeMessage(t, ws, runnerproto.Ping{})
	_, _, err := ws.Read(t.Context())
	if got := websocket.CloseStatus(err); got != runnerproto.CloseProtocolMismatch {
		t.Fatalf("close status = %d (%v), want %d", got, err, runnerproto.CloseProtocolMismatch)
	}
}

func TestPingTimeoutMarksRunnerDisconnected(t *testing.T) {
	f := newFixture(t)
	f.hub.pingTimeout = 400 * time.Millisecond
	disconnects := make(chan string, 1)
	f.hub.OnDisconnect(func(name string) { disconnects <- name })

	ws := f.handshake(defaultHello())
	for range 8 {
		writeMessage(t, ws, runnerproto.Ping{Active: 1})
		time.Sleep(100 * time.Millisecond)
	}
	select {
	case <-disconnects:
		t.Fatal("a runner that pings was dropped")
	default:
	}
	if info := f.hub.Runners(); len(info) != 1 || info[0].Active != 1 || !info[0].Connected {
		t.Fatalf("Runners() = %+v", info)
	}

	if name := waitFor(t, disconnects, "OnDisconnect"); name != testRunner {
		t.Errorf("OnDisconnect(%q)", name)
	}
	if _, ok := f.hub.Get(testRunner); ok {
		t.Error("a silent runner is still available")
	}
	if info := f.hub.Runners(); len(info) != 1 || info[0].Connected {
		t.Errorf("Runners() = %+v", info)
	}
}

func TestBackpressureClosesTheConnection(t *testing.T) {
	f := newFixture(t)
	f.hub.maxOutbox = 64 << 10
	disconnects := make(chan string, 1)
	f.hub.OnDisconnect(func(name string) { disconnects <- name })

	ws := f.handshake(defaultHello())
	r := remote(t, f.hub)

	opened := make(chan runner.PTY, 1)
	go func() {
		pty, err := r.OpenTerminal(context.Background(), "sb", "web01")
		if err != nil {
			t.Error(err)
		}
		opened <- pty
	}()
	call, ok := readMessage(t, ws).(runnerproto.Call)
	if !ok || call.Method != runnerproto.MethodOpenTerminal {
		t.Fatalf("got %+v, want open_terminal", call)
	}
	result, _ := runnerproto.NewResult(call.ID, runnerproto.StreamResult{Stream: 1}, nil)
	writeMessage(t, ws, result)
	pty := waitFor(t, opened, "open_terminal")

	chunk := make([]byte, 32<<10)
	var err error
	for written := 0; written < 1<<30 && err == nil; written += len(chunk) {
		_, err = pty.Write(chunk)
	}
	if err == nil {
		t.Fatal("writing to a runner that never reads kept succeeding")
	}
	waitFor(t, disconnects, "OnDisconnect after the outbox overflowed")
}

func TestStreamFramesAndUnknownStreams(t *testing.T) {
	f := newFixture(t)
	ws := f.handshake(defaultHello())
	r := remote(t, f.hub)

	opened := make(chan runner.PTY, 1)
	go func() {
		pty, err := r.OpenTerminal(context.Background(), "sb", "web01")
		if err != nil {
			t.Error(err)
		}
		opened <- pty
	}()
	call := readMessage(t, ws).(runnerproto.Call)
	result, _ := runnerproto.NewResult(call.ID, runnerproto.StreamResult{Stream: 7}, nil)
	writeMessage(t, ws, result)
	if err := ws.Write(t.Context(), websocket.MessageBinary, runnerproto.EncodeStreamFrame(7, []byte("prompt$ "))); err != nil {
		t.Fatal(err)
	}
	pty := waitFor(t, opened, "open_terminal")
	buf := make([]byte, 64)
	n, err := pty.Read(buf)
	if err != nil || string(buf[:n]) != "prompt$ " {
		t.Fatalf("Read() = %q, %v", buf[:n], err)
	}

	if _, err := pty.Write([]byte("ls\r")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	defer cancel()
	kind, data, err := ws.Read(ctx)
	if err != nil || kind != websocket.MessageBinary {
		t.Fatalf("read frame: %v %v", kind, err)
	}
	if id, payload, _ := runnerproto.DecodeStreamFrame(data); id != 7 || string(payload) != "ls\r" {
		t.Errorf("frame = %d %q", id, payload)
	}

	if err := ws.Write(t.Context(), websocket.MessageBinary, runnerproto.EncodeStreamFrame(99, []byte("stray"))); err != nil {
		t.Fatal(err)
	}
	closeCall, ok := readMessage(t, ws).(runnerproto.Call)
	if !ok || closeCall.Method != runnerproto.MethodCloseTerminal || string(closeCall.Params) != `{"stream":99}` {
		t.Errorf("stray frame answered with %+v, want close_terminal for stream 99", closeCall)
	}

	writeMessage(t, ws, runnerproto.StreamClosed{Stream: 7})
	if _, err := io.ReadAll(pty); err != nil {
		t.Errorf("Read() after stream_closed = %v, want EOF", err)
	}
	if _, err := pty.Write([]byte("x")); !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("Write() after stream_closed = %v", err)
	}
}

func TestNewConnectionReplacesTheOld(t *testing.T) {
	f := newFixture(t)
	disconnects := make(chan string, 2)
	f.hub.OnDisconnect(func(name string) { disconnects <- name })

	first := f.handshake(defaultHello())
	firstRemote := remote(t, f.hub)
	f.handshake(defaultHello())

	_, _, err := first.Read(t.Context())
	if got := websocket.CloseStatus(err); got != websocket.StatusGoingAway {
		t.Errorf("old connection closed with %d (%v), want going away", got, err)
	}
	waitFor(t, disconnects, "OnDisconnect for the replaced connection")
	deadline := time.Now().Add(waitTimeout)
	for {
		if r, ok := f.hub.Get(testRunner); ok && r != firstRemote {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Get() does not return the new connection")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := firstRemote.GC(t.Context()); !errors.Is(err, ErrDisconnected) {
		t.Errorf("GC() on the replaced connection = %v, want ErrDisconnected", err)
	}
}

func TestRegisterValidatesNames(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	for _, name := range []string{"", "ab", "-lab", "Lab01", "lab 01", strings.Repeat("a", 33)} {
		if _, _, err := Register(t.Context(), st.Runners, name, nil, 1); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Register(%q) error = %v, want ErrInvalidName", name, err)
		}
	}
	record, token, err := Register(t.Context(), st.Runners, "lab-01", nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 43 || strings.ContainsAny(token, "+/=") {
		t.Errorf("token %q is not 32 bytes of unpadded base64url", token)
	}
	if record.TokenHash == token || !strings.HasPrefix(record.TokenHash, "$argon2id$") {
		t.Errorf("token hash = %q", record.TokenHash)
	}
	if len(record.Capabilities) != 1 || record.Capabilities[0] != "container" {
		t.Errorf("default capabilities = %v", record.Capabilities)
	}
	if _, _, err := Register(t.Context(), st.Runners, "lab-01", nil, 1); !errors.Is(err, store.ErrRunnerNameTaken) {
		t.Errorf("duplicate Register() error = %v", err)
	}
}
