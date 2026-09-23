//go:build integration

package docker

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
	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

const (
	testImage = "nsl/node"
	fixtureID = "net-ip-01-link-down"
)

func newProvider(t *testing.T) (*Provider, client.APIClient) {
	t.Helper()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skipf("docker client unavailable: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		t.Skipf("docker daemon unreachable: %v", err)
	}
	return New(cli, Options{Image: testImage}), cli
}

func attemptID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random attempt id: %v", err)
	}
	return "t4" + hex.EncodeToString(b)
}

func fixtureSpec(t *testing.T, attempt string) runner.SandboxSpec {
	t.Helper()
	labs, err := content.Load(contenttest.Dir())
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	i := slices.IndexFunc(labs, func(lab content.Lab) bool { return lab.Id == fixtureID })
	if i < 0 {
		t.Fatalf("fixture lab %s not found", fixtureID)
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
	spec, err := runner.SpecFromLab(attempt, testImage, lab, resolved, setup)
	if err != nil {
		t.Fatalf("SpecFromLab: %v", err)
	}
	return spec
}

func mustExec(t *testing.T, p *Provider, sb runner.SandboxID, node string, cmd ...string) runner.ExecResult {
	t.Helper()
	result, err := p.Exec(t.Context(), sb, node, cmd, runner.ExecOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("exec %v on %s: %v", cmd, node, err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exec %v on %s exited with %d: %s", cmd, node, result.ExitCode, result.Stderr)
	}
	return result
}

func waitForAddress(t *testing.T, p *Provider, sb runner.SandboxID, node, address string) string {
	t.Helper()
	var out string
	for range 20 {
		out = string(mustExec(t, p, sb, node, "ip", "-4", "-br", "addr", "show", "eth1").Stdout)
		if strings.Contains(out, address) {
			return out
		}
		time.Sleep(250 * time.Millisecond)
	}
	return out
}

func TestSandboxLifecycle(t *testing.T) {
	p, cli := newProvider(t)
	attempt := attemptID(t)
	spec := fixtureSpec(t, attempt)
	t.Cleanup(func() {
		if err := p.Destroy(context.WithoutCancel(t.Context()), runner.SandboxID(attempt)); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})

	start := time.Now()
	sb, err := p.Provision(t.Context(), spec)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	t.Logf("provision took %s", time.Since(start).Round(time.Millisecond))

	containers, err := cli.ContainerList(t.Context(), container.ListOptions{All: true, Filters: attemptFilter(attempt)})
	if err != nil {
		t.Fatalf("list containers: %v", err)
	}
	if len(containers) != 2 {
		t.Fatalf("got %d containers, want 2", len(containers))
	}
	for _, c := range containers {
		if c.Labels[labelManaged] != "true" || c.Labels[labelAttempt] != attempt || c.Labels[labelNode] == "" {
			t.Errorf("container %s labels = %v", c.ID, c.Labels)
		}
	}

	if out := string(mustExec(t, p, sb, "web01", "ip", "-br", "link", "show", "eth1").Stdout); !strings.Contains(out, "DOWN") {
		t.Errorf("web01 eth1 = %q, want DOWN after setup", out)
	}
	if out := string(mustExec(t, p, sb, "db01", "ip", "-br", "link", "show", "eth1").Stdout); !strings.Contains(out, "UP") {
		t.Errorf("db01 eth1 = %q, want UP", out)
	}
	if out := string(mustExec(t, p, sb, "db01", "ip", "-4", "-br", "addr", "show", "eth1").Stdout); !strings.Contains(out, "10.0.5.20") {
		t.Errorf("db01 eth1 address = %q, want 10.0.5.20", out)
	}
	if out := string(mustExec(t, p, sb, "web01", "cat", "/etc/netplan/50-nsl.yaml").Stdout); !strings.Contains(out, "10.0.5.10/24") {
		t.Errorf("web01 netplan = %q, want 10.0.5.10/24 on eth1", out)
	}
	mustExec(t, p, sb, "web01", "ip", "link", "set", "eth1", "up")
	if out := waitForAddress(t, p, sb, "web01", "10.0.5.10"); !strings.Contains(out, "10.0.5.10") {
		t.Errorf("web01 eth1 address = %q, want 10.0.5.10 once the link is up again", out)
	}
	if out := string(mustExec(t, p, sb, "web01", "readlink", "/etc/resolv.conf").Stdout); strings.TrimSpace(out) == "" {
		t.Error("web01 /etc/resolv.conf is not a symlink")
	}

	t.Run("terminal", func(t *testing.T) { testTerminal(t, p, sb) })
	t.Run("exec timeout", func(t *testing.T) { testExecTimeout(t, p, sb) })
	t.Run("destroy", func(t *testing.T) { testDestroy(t, p, cli, attempt) })
}

func testTerminal(t *testing.T, p *Provider, sb runner.SandboxID) {
	term, err := p.OpenTerminal(t.Context(), sb, "web01")
	if err != nil {
		t.Fatalf("open terminal: %v", err)
	}
	defer term.Close()

	if _, err := term.Write([]byte("echo nsl-pty-ok\n")); err != nil {
		t.Fatalf("write to terminal: %v", err)
	}

	found := make(chan string, 1)
	go func() {
		var seen strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := term.Read(buf)
			seen.Write(buf[:n])
			if strings.Count(seen.String(), "nsl-pty-ok") >= 2 {
				found <- seen.String()
				return
			}
			if err != nil {
				found <- seen.String()
				return
			}
		}
	}()

	select {
	case out := <-found:
		if strings.Count(out, "nsl-pty-ok") < 2 {
			t.Fatalf("terminal output = %q, want the command echo and its output", out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("terminal produced no output within 5s")
	}

	if err := term.Resize(120, 40); err != nil {
		t.Errorf("resize: %v", err)
	}
	if err := term.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
}

func testExecTimeout(t *testing.T, p *Provider, sb runner.SandboxID) {
	result, err := p.Exec(t.Context(), sb, "web01", []string{"sleep", "5"}, runner.ExecOptions{Timeout: time.Second})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if !result.TimedOut {
		t.Errorf("result = %+v, want TimedOut", result)
	}
}

func testDestroy(t *testing.T, p *Provider, cli client.APIClient, attempt string) {
	if err := p.Destroy(t.Context(), runner.SandboxID(attempt)); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	assertNothingLeft(t, cli, attemptFilter(attempt))

	if err := p.Destroy(t.Context(), runner.SandboxID(attempt)); err != nil {
		t.Fatalf("second destroy: %v", err)
	}
}

func assertNothingLeft(t *testing.T, cli client.APIClient, f filters.Args) {
	t.Helper()
	containers, err := cli.ContainerList(t.Context(), container.ListOptions{All: true, Filters: f})
	if err != nil {
		t.Fatalf("list containers: %v", err)
	}
	if len(containers) != 0 {
		t.Errorf("%d containers left behind", len(containers))
	}
	networks, err := cli.NetworkList(t.Context(), network.ListOptions{Filters: f})
	if err != nil {
		t.Fatalf("list networks: %v", err)
	}
	if len(networks) != 0 {
		t.Errorf("%d networks left behind", len(networks))
	}
}

func TestGC(t *testing.T) {
	p, cli := newProvider(t)
	attempt := attemptID(t)
	labels := attemptLabels(attempt)

	if _, err := cli.NetworkCreate(t.Context(), mgmtNetworkName(attempt), network.CreateOptions{Labels: labels}); err != nil {
		t.Fatalf("create stray network: %v", err)
	}
	created, err := cli.ContainerCreate(t.Context(),
		&container.Config{Image: testImage, Labels: nodeLabels(attempt, "stray", "ubuntu")},
		&container.HostConfig{}, nil, nil, containerName(attempt, "stray"))
	if err != nil {
		t.Fatalf("create stray container: %v", err)
	}
	t.Cleanup(func() {
		_ = cli.ContainerRemove(context.WithoutCancel(t.Context()), created.ID, container.RemoveOptions{Force: true, RemoveVolumes: true})
	})

	if err := p.GC(t.Context()); err != nil {
		t.Fatalf("gc: %v", err)
	}
	assertNothingLeft(t, cli, attemptFilter(attempt))
}

func TestHealth(t *testing.T) {
	p, _ := newProvider(t)
	health := p.Health(t.Context())
	if !health.OK || !health.Docker || !health.Image {
		t.Fatalf("health = %+v", health)
	}
	if health.ImageName != testImage {
		t.Errorf("image name = %q", health.ImageName)
	}
	if health.MemAvailableMB <= 0 {
		t.Errorf("MemAvailableMB = %d", health.MemAvailableMB)
	}
}
