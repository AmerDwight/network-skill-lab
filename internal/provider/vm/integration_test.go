//go:build vm_integration

package vm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/content/contenttest"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
)

const (
	vmFixture        = "net-ip-01-vm"
	provisionTimeout = 5 * time.Minute
)

func integrationProvider(t *testing.T) *Provider {
	t.Helper()
	image := os.Getenv("NSL_VM_IMAGE")
	if image == "" {
		t.Skip("NSL_VM_IMAGE is not set")
	}
	dir := os.Getenv("NSL_VM_DIR")
	if dir == "" {
		dir = "/var/tmp/nsl-vm"
	}
	suffix, err := randomHex(4)
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(Config{Image: image, Dir: dir, Instance: "test-" + suffix}, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := p.GC(context.Background()); err != nil {
			t.Errorf("cleanup gc: %v", err)
		}
	})
	return p
}

func vmFixtureSpec(t *testing.T, attempt string) (runner.SandboxSpec, map[string]string) {
	t.Helper()
	labs, err := content.Load(filepath.Join(contenttest.Dir(), "vm"))
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	i := slices.IndexFunc(labs, func(lab content.Lab) bool { return lab.Id == vmFixture })
	if i < 0 {
		t.Fatalf("fixture lab %s not found", vmFixture)
	}
	lab := labs[i]
	if lab.Environment != string(runner.EnvVM) {
		t.Fatalf("fixture environment = %q, want vm", lab.Environment)
	}
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
	return spec, resolved.Params
}

func mustExec(t *testing.T, p *Provider, sb runner.SandboxID, node string, cmd ...string) string {
	t.Helper()
	result, err := p.Exec(t.Context(), sb, node, cmd, runner.ExecOptions{Timeout: 20 * time.Second})
	if err != nil {
		t.Fatalf("exec %v on %s: %v", cmd, node, err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exec %v on %s exited with %d: %s", cmd, node, result.ExitCode, result.Stderr)
	}
	return string(result.Stdout)
}

func TestVMSandboxLifecycle(t *testing.T) {
	p := integrationProvider(t)
	spec, params := vmFixtureSpec(t, "it"+strings.ReplaceAll(p.cfg.Instance, "test-", ""))
	var (
		mu    sync.Mutex
		steps []string
	)
	spec.Progress = func(step string) {
		mu.Lock()
		steps = append(steps, step)
		mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(t.Context(), provisionTimeout)
	defer cancel()
	start := time.Now()
	sb, err := p.Provision(ctx, spec)
	if err != nil {
		t.Fatalf("Provision() error = %v", err)
	}
	t.Logf("cold start of %d VMs (define to setup done): %s", len(spec.Nodes), time.Since(start).Round(100*time.Millisecond))
	if want := []string{"networks", "nodes", "bootstrap", "setup"}; !slices.Equal(steps, want) {
		t.Errorf("progress = %v, want %v", steps, want)
	}

	for _, node := range spec.Nodes {
		name := domainName(p.cfg.Instance, spec.AttemptID, node.Name)
		if err := agentPing(t.Context(), p.virsh, name); err != nil {
			t.Errorf("guest agent on %s: %v", node.Name, err)
		}
		if host := strings.TrimSpace(mustExec(t, p, sb, node.Name, "hostname")); host != node.Name {
			t.Errorf("hostname on %s = %q", node.Name, host)
		}
	}

	addressA, addressB := params["ip_a"], params["ip_b"]
	if out := mustExec(t, p, sb, "db01", "ip", "-br", "addr", "show", "eth1"); !strings.Contains(out, addressB+"/24") || !strings.Contains(out, "UP") {
		t.Errorf("db01 eth1 = %q, want UP with %s/24", out, addressB)
	}
	if out := mustExec(t, p, sb, "web01", "ip", "-br", "addr", "show", "eth1"); !strings.Contains(out, "DOWN") {
		t.Errorf("web01 eth1 = %q, want DOWN after setup", out)
	}
	mustExec(t, p, sb, "web01", "ip", "link", "set", "eth1", "up")
	var out string
	for range 40 {
		out = mustExec(t, p, sb, "web01", "ip", "-br", "addr", "show", "eth1")
		if strings.Contains(out, addressA+"/24") {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !strings.Contains(out, addressA+"/24") {
		t.Errorf("web01 eth1 = %q, want %s/24 once the link is up", out, addressA)
	}
	for range 20 {
		result, err := p.Exec(t.Context(), sb, "web01", []string{"ping", "-c", "1", "-W", "1", addressB}, runner.ExecOptions{Timeout: 5 * time.Second})
		if err == nil && result.ExitCode == 0 {
			out = ""
			break
		}
		out = string(result.Stdout) + string(result.Stderr)
	}
	if out != "" {
		t.Errorf("web01 cannot ping db01 over the link: %s", out)
	}
	if user := strings.TrimSpace(func() string {
		result, err := p.Exec(t.Context(), sb, "web01", []string{"id", "-un"}, runner.ExecOptions{User: "nsl"})
		if err != nil {
			t.Fatal(err)
		}
		return string(result.Stdout)
	}()); user != "nsl" {
		t.Errorf("exec as nsl ran as %q", user)
	}

	t.Run("exec timeout", func(t *testing.T) {
		begin := time.Now()
		result, err := p.Exec(t.Context(), sb, "db01", []string{"sleep", "30"}, runner.ExecOptions{Timeout: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if !result.TimedOut || time.Since(begin) > 10*time.Second {
			t.Errorf("result = %+v after %s, want a timeout after about 1s", result, time.Since(begin))
		}
	})
	t.Run("missing node", func(t *testing.T) {
		if _, err := p.Exec(t.Context(), sb, "nope", []string{"true"}, runner.ExecOptions{}); !errors.Is(err, runner.ErrNodeNotFound) {
			t.Errorf("Exec(unknown node) error = %v", err)
		}
	})
	t.Run("terminal", func(t *testing.T) { testTerminal(t, p, sb) })
	t.Run("destroy", func(t *testing.T) { testDestroy(t, p, sb) })
}

func testTerminal(t *testing.T, p *Provider, sb runner.SandboxID) {
	term, err := p.OpenTerminal(t.Context(), sb, "web01")
	if err != nil {
		t.Fatalf("OpenTerminal() error = %v", err)
	}
	defer func() { _ = term.Close() }()
	if err := term.Resize(120, 40); err != nil {
		t.Errorf("Resize() error = %v", err)
	}
	if _, err := term.Write([]byte("echo nsl-pty-$((6*7)) $(whoami) $(tput cols)\n")); err != nil {
		t.Fatalf("write to terminal: %v", err)
	}

	found := make(chan string, 1)
	go func() {
		var seen strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := term.Read(buf)
			seen.Write(buf[:n])
			if strings.Contains(seen.String(), "nsl-pty-42 nsl 120") || err != nil {
				found <- seen.String()
				return
			}
		}
	}()
	select {
	case got := <-found:
		if !strings.Contains(got, "nsl-pty-42 nsl 120") {
			t.Errorf("terminal output = %q, want nsl-pty-42 nsl 120", got)
		}
	case <-time.After(20 * time.Second):
		t.Error("no echo from the terminal within 20s")
	}
}

func testDestroy(t *testing.T, p *Provider, sb runner.SandboxID) {
	if err := p.Destroy(t.Context(), sb); err != nil {
		t.Fatalf("Destroy() error = %v", err)
	}
	inv, err := p.owned(t.Context(), p.instanceMatch)
	if err != nil {
		t.Fatal(err)
	}
	if !inv.empty() {
		t.Errorf("left behind after Destroy: %+v", inv)
	}
	prefix := namePrefix + p.cfg.Instance + "-"
	for _, list := range [][]string{{"list", "--all", "--name"}, {"net-list", "--all", "--name"}} {
		names, err := p.listNames(t.Context(), list...)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			if strings.HasPrefix(name, prefix) {
				t.Errorf("%s still lists %s", list[0], name)
			}
		}
	}
	if _, err := os.Stat(sandboxDir(p.cfg.Dir, p.cfg.Instance, string(sb))); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("sandbox dir still present: %v", err)
	}
	if err := p.Destroy(t.Context(), sb); !errors.Is(err, runner.ErrSandboxNotFound) {
		t.Errorf("second Destroy() error = %v, want ErrSandboxNotFound", err)
	}
	if _, err := p.Exec(t.Context(), sb, "web01", []string{"true"}, runner.ExecOptions{}); !errors.Is(err, runner.ErrSandboxNotFound) {
		t.Errorf("Exec() after Destroy error = %v, want ErrSandboxNotFound", err)
	}
}
