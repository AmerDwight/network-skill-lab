//go:build vm_integration

package runnerclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/content/contenttest"
	"github.com/AmerDwight/network-skill-lab/internal/provider/vm"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
)

func vmSuffix(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func vmProvider(t *testing.T) *vm.Provider {
	t.Helper()
	image := os.Getenv("NSL_VM_IMAGE")
	if image == "" {
		t.Skip("NSL_VM_IMAGE is not set")
	}
	dir := os.Getenv("NSL_VM_DIR")
	if dir == "" {
		dir = "/var/tmp/nsl-vm"
	}
	p, err := vm.New(vm.Config{Image: image, Dir: dir, Instance: "test-runner-" + vmSuffix(t)}, quiet)
	if err != nil {
		t.Fatalf("vm.New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := p.GC(context.Background()); err != nil {
			t.Errorf("cleanup gc: %v", err)
		}
	})
	return p
}

func vmFixtureSpec(t *testing.T, attempt string) runner.SandboxSpec {
	t.Helper()
	labs, err := content.Load(filepath.Join(contenttest.Dir(), "vm"))
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	i := slices.IndexFunc(labs, func(lab content.Lab) bool { return lab.Id == "net-ip-01-vm" })
	if i < 0 {
		t.Fatal("vm fixture lab not found")
	}
	lab := labs[i]
	resolved, err := lab.ResolveFor(1)
	if err != nil {
		t.Fatalf("resolve params: %v", err)
	}
	setup, err := os.ReadFile(filepath.Join(lab.Dir, lab.Setup))
	if err != nil {
		t.Fatalf("read setup script: %v", err)
	}
	spec, err := runner.SpecFromLab(attempt, "", lab, resolved, setup)
	if err != nil {
		t.Fatalf("SpecFromLab: %v", err)
	}
	return spec
}

func TestRemoteRunnerWithVM(t *testing.T) {
	provider := vmProvider(t)
	h := newHarness(t)
	c := newClient(Options{URL: h.url, Token: h.token, Name: runnerName, Capacity: 2, Version: "test", Provider: provider, Logger: quiet})
	h.start(c)
	remote := h.waitRunner()
	if infos := h.hub.Runners(); len(infos) != 1 || !slices.Equal(infos[0].Capabilities, []runner.Env{runner.EnvVM}) {
		t.Fatalf("hub sees %+v, want one runner advertising vm", infos)
	}

	spec := vmFixtureSpec(t, "t4"+vmSuffix(t))
	var steps []string
	spec.Progress = func(step string) { steps = append(steps, step) }
	start := time.Now()
	sb, err := remote.Provision(t.Context(), spec)
	if err != nil {
		t.Fatalf("Provision() error = %v", err)
	}
	t.Logf("vm provision through the runner took %s with steps %v", time.Since(start).Round(100*time.Millisecond), steps)
	if want := []string{"networks", "nodes", "bootstrap", "setup"}; !slices.Equal(steps, want) {
		t.Errorf("progress = %v, want %v", steps, want)
	}

	result, err := remote.Exec(t.Context(), sb, "db01", []string{"bash", "-s"}, runner.ExecOptions{
		Stdin:   strings.NewReader("echo remote-exec-$((6*7)) $(hostname)\n"),
		Timeout: 10 * time.Second,
	})
	if err != nil || result.ExitCode != 0 || strings.TrimSpace(string(result.Stdout)) != "remote-exec-42 db01" {
		t.Fatalf("Exec() = %+v, %v", result, err)
	}

	if health := remote.Health(t.Context()); !health.OK || !health.Image {
		t.Errorf("Health() = %+v", health)
	}

	pty, err := remote.OpenTerminal(t.Context(), sb, "web01")
	if err != nil {
		t.Fatalf("OpenTerminal() error = %v", err)
	}
	if err := pty.Resize(100, 30); err != nil {
		t.Errorf("Resize() error = %v", err)
	}
	if _, err := pty.Write([]byte("echo nsl-remote-$((1+1)) $(hostname)\r")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, pty, "nsl-remote-2 web01")
	_ = pty.Close()

	if err := remote.Destroy(t.Context(), sb); err != nil {
		t.Fatalf("Destroy() error = %v", err)
	}
	if _, err := provider.Exec(t.Context(), sb, "web01", []string{"true"}, runner.ExecOptions{}); err == nil {
		t.Error("the sandbox still exists after Destroy")
	}
}
