//go:build integration

package e2e

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/api"
	"github.com/AmerDwight/network-skill-lab/internal/attempt"
	"github.com/AmerDwight/network-skill-lab/internal/auth"
	"github.com/AmerDwight/network-skill-lab/internal/checker"
	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/content/contenttest"
	"github.com/AmerDwight/network-skill-lab/internal/provider/docker"
	"github.com/AmerDwight/network-skill-lab/internal/recorder"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/runnerclient"
	"github.com/AmerDwight/network-skill-lab/internal/runnerserver"
	"github.com/AmerDwight/network-skill-lab/internal/store"
	"github.com/coder/websocket"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

const remoteRunner = "e2e-remote"

// cutProxy forwards TCP to the control plane and can drop every open connection at
// once, which a runner sees as a network failure rather than a shutdown.
type cutProxy struct {
	listener net.Listener
	target   string

	mu    sync.Mutex
	conns []net.Conn
}

func newCutProxy(t *testing.T, target string) *cutProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &cutProxy{listener: listener, target: target}
	t.Cleanup(func() {
		_ = listener.Close()
		p.cut()
	})
	go p.serve()
	return p
}

func (p *cutProxy) serve() {
	for {
		in, err := p.listener.Accept()
		if err != nil {
			return
		}
		out, err := net.Dial("tcp", p.target)
		if err != nil {
			_ = in.Close()
			continue
		}
		p.mu.Lock()
		p.conns = append(p.conns, in, out)
		p.mu.Unlock()
		go func() { _, _ = io.Copy(out, in); _ = out.Close() }()
		go func() { _, _ = io.Copy(in, out); _ = in.Close() }()
	}
}

func (p *cutProxy) cut() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.conns {
		_ = c.Close()
	}
	p.conns = nil
}

func instanceContainers(t *testing.T, cli client.APIClient, instance string) int {
	t.Helper()
	f := filters.NewArgs(filters.Arg("label", "nsl.managed=true"), filters.Arg("label", "nsl.instance="+instance))
	containers, err := cli.ContainerList(context.WithoutCancel(t.Context()), container.ListOptions{All: true, Filters: f})
	if err != nil {
		t.Fatalf("list containers: %v", err)
	}
	return len(containers)
}

func readTerminalUntil(t *testing.T, conn *websocket.Conn, want string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	var seen strings.Builder
	for !strings.Contains(seen.String(), want) {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read terminal while waiting for %q: %v (seen %q)", want, err, seen.String())
		}
		if kind == websocket.MessageBinary {
			seen.Write(data)
		}
	}
}

func TestLocalAndRemoteRunnersEachRunAnAttempt(t *testing.T) {
	cli := dockerClient(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.WithoutCancel(t.Context())

	loaded, err := content.LoadAll(contenttest.Dir())
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	localInstance := "e2e-local-" + store.NewID()
	remoteInstance := "e2e-remote-" + store.NewID()
	local := docker.New(cli, docker.Options{Image: testImage, Instance: localInstance})
	remote := docker.New(cli, docker.Options{Image: testImage, Instance: remoteInstance})
	t.Cleanup(func() {
		for _, p := range []*docker.Provider{local, remote} {
			if err := p.GC(ctx); err != nil {
				t.Logf("cleanup gc: %v", err)
			}
		}
	})

	hub := runnerserver.NewHub(st.Runners, logger)
	attempts := attempt.New(attempt.Deps{
		Store:   st,
		Runners: runner.NewRegistry(local, 1, hub),
		Content: loaded,
		Image:   testImage,
		DataDir: dataDir,
		Logger:  logger,
	})
	t.Cleanup(attempts.Close)
	hub.OnDisconnect(attempts.RunnerDisconnected)
	hub.OnReconnect(attempts.RunnerReconnected)

	checks := checker.New(checker.Deps{Store: st, Runner: attempts.Runner(), Attempts: attempts, Interval: time.Second, Logger: logger})
	t.Cleanup(checks.Close)
	attempts.SetSweeper(checks)
	recordings := recorder.New(recorder.Deps{Store: st, Runner: attempts.Runner(), Attempts: attempts, Interval: time.Second, DataDir: dataDir, Logger: logger})
	t.Cleanup(recordings.Close)
	checks.Start(t.Context())
	recordings.Start(t.Context())

	server := httptest.NewServer(api.New(api.Deps{
		Attempts: attempts,
		Content:  loaded,
		Store:    st,
		Runner:   attempts.Runner(),
		Hub:      hub,
		Recorder: recordings,
		Auth:     auth.New(st),
		Logger:   logger,
	}))
	t.Cleanup(server.Close)
	s := &stack{server: server, attempts: attempts, provider: local, store: st, dataDir: dataDir, users: map[string]store.User{}}

	admin := s.newUser(t, "root")
	if err := st.Users.SetRole(ctx, s.users["root"].ID, store.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	created := s.requestAs(t, admin, http.MethodPost, "/api/admin/runners", `{"name":"`+remoteRunner+`"}`)
	if created["_status"] != float64(http.StatusCreated) {
		t.Fatalf("create runner: %v", created)
	}

	proxy := newCutProxy(t, strings.TrimPrefix(server.URL, "http://"))
	runnerCtx, stopRunner := context.WithCancel(ctx)
	t.Cleanup(stopRunner)
	runnerDone := make(chan error, 1)
	go func() {
		runnerDone <- runnerclient.Run(runnerCtx, runnerclient.Options{
			URL:      "ws://" + proxy.listener.Addr().String() + "/ws/runner",
			Token:    created["token"].(string),
			Name:     remoteRunner,
			Capacity: 1,
			Version:  "e2e",
			Provider: remote,
			Logger:   logger,
		})
	}()
	connected := func() bool {
		_, ok := hub.Get(remoteRunner)
		return ok
	}
	waitFor(t, "the remote runner to connect", 15*time.Second, connected)

	alice, bob := s.newUser(t, "alice"), s.newUser(t, "bob")
	start := func(client *http.Client) string {
		t.Helper()
		got := s.requestAs(t, client, http.MethodPost, "/api/attempts", `{"lab_id":"`+fixtureLab+`","mode":"guided"}`)
		if got["_status"] != float64(http.StatusCreated) {
			t.Fatalf("start attempt: %v", got)
		}
		id := got["id"].(string)
		t.Cleanup(func() {
			if _, err := attempts.AbandonAsAdmin(ctx, id); err != nil && !errors.Is(err, attempt.ErrTerminal) {
				t.Errorf("cleanup abandon: %v", err)
			}
		})
		return id
	}
	onRemote, onLocal := start(alice), start(bob)
	s.waitForStatus(t, alice, onRemote, store.StatusRunning)
	s.waitForStatus(t, bob, onLocal, store.StatusRunning)

	remoteView := s.requestAs(t, alice, http.MethodGet, "/api/attempts/"+onRemote, "")
	localView := s.requestAs(t, bob, http.MethodGet, "/api/attempts/"+onLocal, "")
	if remoteView["runner"] != remoteRunner || localView["runner"] != runner.LocalName {
		t.Fatalf("runners = %v and %v, want %s and %s", remoteView["runner"], localView["runner"], remoteRunner, runner.LocalName)
	}
	if n := instanceContainers(t, cli, remoteInstance); n == 0 {
		t.Error("no containers carry the remote runner's instance label")
	}
	if n := instanceContainers(t, cli, localInstance); n == 0 {
		t.Error("no containers carry the local runner's instance label")
	}

	term := s.dialAs(t, alice, "/ws/attempts/"+onRemote+"/term/web01/main")
	writeCtx, cancelWrite := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancelWrite()
	if err := term.Write(writeCtx, websocket.MessageBinary, []byte("echo nsl-remote-$((40+2))\r")); err != nil {
		t.Fatalf("write to the remote terminal: %v", err)
	}
	readTerminalUntil(t, term, "nsl-remote-42")
	_ = term.CloseNow()

	stats := s.requestAs(t, admin, http.MethodGet, "/api/admin/stats", "")
	if stats["sandboxes_active"] != float64(2) || stats["sandboxes_max"] != float64(2) {
		t.Errorf("stats = %v", stats)
	}

	cutAt := time.Now()
	proxy.cut()
	waitFor(t, "the remote attempt to fail", 10*time.Second, func() bool {
		view, err := attempts.Get(ctx, onRemote)
		return err == nil && view.Status == store.StatusError
	})
	t.Logf("the remote attempt failed %s after the connection dropped", time.Since(cutAt).Round(time.Millisecond))
	if view, _ := attempts.Get(ctx, onRemote); view.ErrorMessage != attempt.ReasonRunnerDisconnected {
		t.Errorf("error message = %q, want %q", view.ErrorMessage, attempt.ReasonRunnerDisconnected)
	}
	if view, _ := attempts.Get(ctx, onLocal); view.Status != store.StatusRunning {
		t.Errorf("local attempt is %s after the remote runner dropped", view.Status)
	}

	waitFor(t, "the remote runner to reconnect", 30*time.Second, connected)
	waitFor(t, "the remote sandbox to be destroyed", 60*time.Second, func() bool {
		return instanceContainers(t, cli, remoteInstance) == 0
	})
	if n := instanceContainers(t, cli, localInstance); n == 0 {
		t.Error("the local sandbox went away with the remote one")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, server.URL+"/api/admin/runners/"+created["id"].(string), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke: status = %d", resp.StatusCode)
	}
	select {
	case err := <-runnerDone:
		if !errors.Is(err, runnerclient.ErrUnauthorized) {
			t.Errorf("runner stopped with %v, want ErrUnauthorized", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("revoking the token did not stop the runner")
	}
	if connected() {
		t.Error("the hub still holds the revoked runner")
	}
}
