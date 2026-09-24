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

// testInstance keeps a test binary from collecting the sandboxes of a running
// server, or of another test binary, on the same daemon.
var testInstance = "test-" + randomSuffix()

func randomSuffix() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func newProvider(t *testing.T) (*Provider, client.APIClient) {
	t.Helper()
	return newProviderFor(t, testInstance)
}

func newProviderFor(t *testing.T, instance string) (*Provider, client.APIClient) {
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
	return New(cli, Options{Image: testImage, Instance: instance}), cli
}

func attemptID(t *testing.T) string {
	t.Helper()
	return "t4" + randomSuffix()
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
		if c.Labels[labelManaged] != "true" || c.Labels[labelInstance] != testInstance ||
			c.Labels[labelAttempt] != attempt || c.Labels[labelNode] == "" {
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

func routedSpec(attempt string) runner.SandboxSpec {
	return runner.SandboxSpec{
		AttemptID: attempt,
		Image:     testImage,
		Internet:  true,
		Nodes: []runner.NodeSpec{
			{Name: "web01", Role: "ubuntu"},
			{Name: "gw01", Role: "ubuntu"},
			{Name: "db01", Role: "ubuntu"},
		},
		Links: []runner.LinkSpec{
			{Name: "link-a", Subnet: "10.0.61.0/24", Endpoints: []runner.EndpointSpec{
				{Node: "web01", Iface: "eth1", Address: "10.0.61.10/24"},
				{Node: "gw01", Iface: "eth1", Address: "10.0.61.2/24"},
			}},
			{Name: "link-b", Subnet: "10.0.62.0/24", Endpoints: []runner.EndpointSpec{
				{Node: "gw01", Iface: "eth2", Address: "10.0.62.2/24"},
				{Node: "db01", Iface: "eth1", Address: "10.0.62.20/24"},
			}},
		},
	}
}

func TestRoutedTopology(t *testing.T) {
	p, _ := newProvider(t)
	attempt := attemptID(t)
	t.Cleanup(func() {
		if err := p.Destroy(context.WithoutCancel(t.Context()), runner.SandboxID(attempt)); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})

	start := time.Now()
	sb, err := p.Provision(t.Context(), routedSpec(attempt))
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	t.Logf("provision took %s", time.Since(start).Round(time.Millisecond))

	for _, node := range []string{"web01", "gw01", "db01"} {
		out := string(mustExec(t, p, sb, node, "ip", "route", "show", "default").Stdout)
		if !strings.Contains(out, "dev eth0") {
			t.Errorf("%s default route = %q, want it via the mgmt interface", node, out)
		}
		if strings.Contains(out, "dev eth1") || strings.Contains(out, "dev eth2") {
			t.Errorf("%s default route = %q, want no default route via a link", node, out)
		}
	}

	mustExec(t, p, sb, "gw01", "sysctl", "-w", "net.ipv4.ip_forward=1")
	mustExec(t, p, sb, "web01", "ip", "route", "add", "10.0.62.0/24", "via", "10.0.61.2")
	mustExec(t, p, sb, "db01", "ip", "route", "add", "10.0.61.0/24", "via", "10.0.62.2")
	mustExec(t, p, sb, "web01", "ping", "-c", "3", "-i", "0.5", "-W", "1", "10.0.62.20")

	if code := pingExitCode(t, p, sb, "web01", "eth1", "1.1.1.1"); code == 0 {
		t.Error("web01 reached 1.1.1.1 over eth1, want a link network with no way out")
	}
	if code := pingExitCode(t, p, sb, "web01", "eth0", "10.0.62.20"); code == 0 {
		t.Error("web01 reached the far link subnet over eth0, want it reachable over the link only")
	}
}

func pingExitCode(t *testing.T, p *Provider, sb runner.SandboxID, node, iface, target string) int {
	t.Helper()
	cmd := []string{"ping", "-c", "1", "-W", "1", "-I", iface, target}
	result, err := p.Exec(t.Context(), sb, node, cmd, runner.ExecOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("ping %s from %s on %s: %v", target, iface, node, err)
	}
	return result.ExitCode
}

func createStray(t *testing.T, cli client.APIClient, instance, attempt string) {
	t.Helper()
	labels := attemptLabels(instance, attempt)
	if _, err := cli.NetworkCreate(t.Context(), mgmtNetworkName(attempt), network.CreateOptions{Labels: labels}); err != nil {
		t.Fatalf("create stray network: %v", err)
	}
	created, err := cli.ContainerCreate(t.Context(),
		&container.Config{Image: testImage, Labels: nodeLabels(instance, attempt, "stray", "ubuntu")},
		&container.HostConfig{}, nil, nil, containerName(attempt, "stray"))
	if err != nil {
		t.Fatalf("create stray container: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.WithoutCancel(t.Context())
		_ = cli.ContainerRemove(ctx, created.ID, container.RemoveOptions{Force: true, RemoveVolumes: true})
		_ = cli.NetworkRemove(ctx, mgmtNetworkName(attempt))
	})
}

func TestGC(t *testing.T) {
	p, cli := newProvider(t)
	attempt := attemptID(t)
	createStray(t, cli, testInstance, attempt)

	if err := p.GC(t.Context()); err != nil {
		t.Fatalf("gc: %v", err)
	}
	assertNothingLeft(t, cli, attemptFilter(attempt))
}

func TestGCLeavesOtherInstancesAlone(t *testing.T) {
	mine, cli := newProviderFor(t, testInstance+"-a")
	myAttempt, theirAttempt := attemptID(t), attemptID(t)
	createStray(t, cli, testInstance+"-a", myAttempt)
	createStray(t, cli, testInstance+"-b", theirAttempt)

	if err := mine.GC(t.Context()); err != nil {
		t.Fatalf("gc: %v", err)
	}
	assertNothingLeft(t, cli, attemptFilter(myAttempt))

	containers, err := cli.ContainerList(t.Context(), container.ListOptions{All: true, Filters: attemptFilter(theirAttempt)})
	if err != nil {
		t.Fatalf("list containers: %v", err)
	}
	if len(containers) != 1 {
		t.Errorf("the other instance has %d containers, want 1", len(containers))
	}
	networks, err := cli.NetworkList(t.Context(), network.ListOptions{Filters: attemptFilter(theirAttempt)})
	if err != nil {
		t.Fatalf("list networks: %v", err)
	}
	if len(networks) != 1 {
		t.Errorf("the other instance has %d networks, want 1", len(networks))
	}

	theirs, _ := newProviderFor(t, testInstance+"-b")
	if err := theirs.GC(t.Context()); err != nil {
		t.Fatalf("gc of the other instance: %v", err)
	}
	assertNothingLeft(t, cli, attemptFilter(theirAttempt))
}

// A network whose containers are still being detached is the startup case of #57:
// GC must report it, and nsl serve must keep running.
func TestGCSurvivesANetworkThatStillHasEndpoints(t *testing.T) {
	p, cli := newProvider(t)
	attempt := attemptID(t)
	spec := fixtureSpec(t, attempt)
	spec.Setup = runner.Script{}
	t.Cleanup(func() {
		if err := p.Destroy(context.WithoutCancel(t.Context()), runner.SandboxID(attempt)); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
	if _, err := p.Provision(t.Context(), spec); err != nil {
		t.Fatalf("provision: %v", err)
	}

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
