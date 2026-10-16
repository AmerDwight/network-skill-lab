//go:build integration

package runnerclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/content/contenttest"
	"github.com/AmerDwight/network-skill-lab/internal/provider/docker"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
	dockerclient "github.com/docker/docker/client"
)

const (
	integrationImage = "nsl/node"
	fixtureLab       = "net-ip-01-link-down"
	latencySamples   = 30
)

func randomHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func dockerProvider(t *testing.T) *docker.Provider {
	t.Helper()
	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		t.Skipf("docker client unavailable: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		t.Skipf("docker daemon unreachable: %v", err)
	}
	return docker.New(cli, docker.Options{Image: integrationImage, Instance: "test-runner-" + randomHex(t)})
}

func fixtureSpec(t *testing.T, attempt string) runner.SandboxSpec {
	t.Helper()
	labs, err := content.Load(contenttest.Dir())
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	i := slices.IndexFunc(labs, func(lab content.Lab) bool { return lab.Id == fixtureLab })
	if i < 0 {
		t.Fatalf("fixture lab %s not found", fixtureLab)
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
	spec, err := runner.SpecFromLab(attempt, integrationImage, lab, resolved, setup)
	if err != nil {
		t.Fatalf("SpecFromLab: %v", err)
	}
	return spec
}

func roundTrip(t *testing.T, pty runner.PTY, i int) time.Duration {
	t.Helper()
	want := fmt.Sprint(40000 + i)
	start := time.Now()
	if _, err := fmt.Fprintf(pty, "echo $((40000+%d))\r", i); err != nil {
		t.Fatalf("write to terminal: %v", err)
	}
	readUntil(t, pty, want)
	return time.Since(start)
}

func median(samples []time.Duration) time.Duration {
	sorted := slices.Clone(samples)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[len(sorted)/2]
}

func TestRemoteRunnerWithDocker(t *testing.T) {
	provider := dockerProvider(t)
	h := newHarness(t)
	c := newClient(Options{URL: h.url, Token: h.token, Name: runnerName, Capacity: 3, Version: "test", Provider: provider, Logger: quiet})
	h.start(c)
	remote := h.waitRunner()

	attempt := "t2" + randomHex(t)
	spec := fixtureSpec(t, attempt)
	var steps []string
	spec.Progress = func(step string) { steps = append(steps, step) }
	t.Cleanup(func() { _ = provider.Destroy(context.WithoutCancel(t.Context()), runner.SandboxID(attempt)) })

	start := time.Now()
	sb, err := remote.Provision(t.Context(), spec)
	if err != nil {
		t.Fatalf("Provision() error = %v", err)
	}
	t.Logf("provision through the runner took %s with steps %v", time.Since(start).Round(time.Millisecond), steps)
	if len(steps) == 0 {
		t.Error("no progress reached the hub")
	}
	node := spec.Nodes[0].Name

	result, err := remote.Exec(t.Context(), sb, node, []string{"bash", "-s"}, runner.ExecOptions{
		Stdin:   strings.NewReader("echo remote-exec-$((6*7))\n"),
		Timeout: 10 * time.Second,
	})
	if err != nil || result.ExitCode != 0 || strings.TrimSpace(string(result.Stdout)) != "remote-exec-42" {
		t.Fatalf("Exec() = %+v, %v", result, err)
	}

	remotePTY, err := remote.OpenTerminal(t.Context(), sb, node)
	if err != nil {
		t.Fatalf("OpenTerminal() error = %v", err)
	}
	defer func() { _ = remotePTY.Close() }()
	if err := remotePTY.Resize(100, 30); err != nil {
		t.Errorf("Resize() error = %v", err)
	}
	if _, err := remotePTY.Write([]byte("echo nsl-remote-$((1+1))\r")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, remotePTY, "nsl-remote-2")

	localPTY, err := provider.OpenTerminal(t.Context(), sb, node)
	if err != nil {
		t.Fatalf("local OpenTerminal() error = %v", err)
	}
	defer func() { _ = localPTY.Close() }()
	if err := localPTY.Resize(100, 30); err != nil {
		t.Errorf("local Resize() error = %v", err)
	}
	readUntil(t, localPTY, "$ ")

	var remoteSamples, localSamples []time.Duration
	for i := range latencySamples {
		localSamples = append(localSamples, roundTrip(t, localPTY, 2*i))
		remoteSamples = append(remoteSamples, roundTrip(t, remotePTY, 2*i+1))
	}

	remoteMedian, localMedian := median(remoteSamples), median(localSamples)
	t.Logf("pty round trip over %d samples: direct median %s, through hub and runner median %s, added %s",
		latencySamples, localMedian, remoteMedian, remoteMedian-localMedian)
	if added := remoteMedian - localMedian; added > 50*time.Millisecond {
		t.Errorf("the runner protocol adds %s to a pty round trip, the target is under 50ms", added)
	}

	if err := remote.Destroy(t.Context(), sb); err != nil {
		t.Fatalf("Destroy() error = %v", err)
	}
	if _, err := provider.Exec(t.Context(), sb, node, []string{"true"}, runner.ExecOptions{}); err == nil {
		t.Error("the sandbox still exists after Destroy")
	}
	if c.active() != 0 {
		t.Errorf("runner still tracks %d sandboxes", c.active())
	}
}
