//go:build content_integration

package e2e

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/attempt"
	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/provider/docker"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/runnerclient"
	"github.com/AmerDwight/network-skill-lab/internal/runnerserver"
	"github.com/AmerDwight/network-skill-lab/internal/store"
	"github.com/docker/docker/client"
)

const (
	contentRemoteEnv      = "NSL_TEST_REMOTE_RUNNER"
	contentRemoteRunner   = "content-remote"
	contentRemoteCapacity = 3
)

// contentBackend is where the content suite's sandboxes run: the in-process docker
// provider by default, or a runner client speaking the runner protocol to the test
// server when NSL_TEST_REMOTE_RUNNER=1, with no local runner to fall back on.
type contentBackend struct {
	provider *docker.Provider
	local    runner.Runner
	registry *runner.Registry
	hub      *runnerserver.Hub
	remote   string
	store    *store.Store
	logger   *slog.Logger
}

func newContentBackend(t *testing.T, cli client.APIClient, st *store.Store, image string, logger *slog.Logger) *contentBackend {
	t.Helper()
	if os.Getenv(contentRemoteEnv) != "1" {
		provider := docker.New(cli, docker.Options{Image: image})
		return &contentBackend{provider: provider, local: provider}
	}

	provider := docker.New(cli, docker.Options{Image: image, Instance: "e2e-content-" + store.NewID()})
	t.Cleanup(func() {
		if err := provider.GC(context.WithoutCancel(t.Context())); err != nil {
			t.Logf("cleanup gc: %v", err)
		}
	})
	hub := runnerserver.NewHub(st.Runners, logger)
	return &contentBackend{
		provider: provider,
		registry: runner.NewRegistry(nil, 0, hub),
		hub:      hub,
		remote:   contentRemoteRunner,
		store:    st,
		logger:   logger,
	}
}

func (b *contentBackend) listen(attempts *attempt.Service) {
	if b.hub == nil {
		return
	}
	b.hub.OnDisconnect(attempts.RunnerDisconnected)
	b.hub.OnReconnect(attempts.RunnerReconnected)
}

func (b *contentBackend) connect(t *testing.T, serverURL string) {
	t.Helper()
	if b.remote == "" {
		return
	}
	_, token, err := runnerserver.Register(t.Context(), b.store.Runners, b.remote, nil, contentRemoteCapacity)
	if err != nil {
		t.Fatalf("register runner: %v", err)
	}
	ctx, stop := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan struct{})
	t.Cleanup(func() {
		stop()
		<-done
	})
	go func() {
		defer close(done)
		err := runnerclient.Run(ctx, runnerclient.Options{
			URL:      "ws" + strings.TrimPrefix(serverURL, "http") + "/ws/runner",
			Token:    token,
			Name:     b.remote,
			Capacity: contentRemoteCapacity,
			Version:  "e2e",
			Provider: b.provider,
			Logger:   b.logger,
		})
		if err != nil && ctx.Err() == nil {
			t.Errorf("runner client: %v", err)
		}
	}()
	contentWaitFor(t, "the remote runner to connect", 15*time.Second, func() bool {
		_, ok := b.hub.Get(b.remote)
		return ok
	})
}

func (b *contentBackend) assertRunner(t *testing.T, view map[string]any) {
	t.Helper()
	if b.remote != "" && view["runner"] != b.remote {
		t.Fatalf("attempt %v landed on runner %v, want the remote runner %s", view["id"], view["runner"], b.remote)
	}
}

func TestContentVMLabsAreSkipped(t *testing.T) {
	loaded, err := content.LoadAll(filepath.Join("..", "..", "content"))
	if err != nil {
		t.Fatalf("load shipped content: %v", err)
	}
	for _, lab := range loaded.Labs {
		if lab.Environment != string(runner.EnvVM) {
			continue
		}
		t.Run(lab.Id, func(t *testing.T) {
			if os.Getenv("NSL_VM_IMAGE") == "" {
				t.Skipf("%s needs environment vm and NSL_VM_IMAGE is not set", lab.Id)
			}
			t.Skipf("%s needs environment vm: the content suite does not start a vm runner, make test-vm covers the vm provider", lab.Id)
		})
	}
}
