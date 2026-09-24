//go:build integration

package docker

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
)

const k3sLabID = "k3s-t4-crashloop"

var k3sLabFiles = map[string]string{
	"lab.yaml": `id: k3s-t4-crashloop
version: 1
title: { zh: "Pod 一直重啟", en: "Pod keeps restarting" }
topic: k3s/pods
level: 2
modes: [guided]
environment: container
estimated_minutes: 5
internet: false
ticket:
  zh: "{{ns}} 的 broken Deployment 沒有可用的 Pod。"
  en: "The broken Deployment in {{ns}} has no available pod."
params:
  ns: { gen: const, value: nsl }
setup: setup.sh
checkpoints:
  - id: deploy-available
    title: { zh: "broken 有可用的 Pod", en: "broken has an available pod" }
    node: k3s01
    script: checks/01-deploy-available.sh
    visible: true
solution: { zh: solution.zh.md, en: solution.en.md }
`,
	"topology.yaml": `nodes:
  k3s01: { role: k3s-server }
  ops01: { role: ubuntu }
links:
  - endpoints: ["k3s01:eth1", "ops01:eth1"]
    subnet: 10.0.9.0/24
    addresses: { k3s01: 10.0.9.10/24, ops01: 10.0.9.20/24 }
`,
	"setup.sh": `#!/usr/bin/env bash
set -euo pipefail

[ "$NSL_NODE" = k3s01 ] || exit 0

kubectl create namespace "$NSL_NS"
kubectl -n "$NSL_NS" create deployment broken \
	--image=docker.io/rancher/mirrored-library-busybox:1.37.0 -- sh -c "exit 1"
`,
	"checks/01-deploy-available.sh": `#!/usr/bin/env bash
set -euo pipefail

[ "$(kubectl -n "$NSL_NS" get deploy broken -o jsonpath='{.status.availableReplicas}')" = 1 ]
`,
	"solution.zh.md": "把 broken 的 command 換成長時間執行的指令。\n",
	"solution.en.md": "Replace the command of broken with one that keeps running.\n",
}

func writeK3sLab(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	labDir := filepath.Join(dir, "labs", k3sLabID)
	for name, body := range k3sLabFiles {
		path := filepath.Join(labDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create %s: %v", filepath.Dir(path), err)
		}
		perm := os.FileMode(0o644)
		if strings.HasSuffix(name, ".sh") {
			perm = 0o755
		}
		if err := os.WriteFile(path, []byte(body), perm); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return dir
}

func k3sSpec(t *testing.T, attempt string) (runner.SandboxSpec, []byte) {
	t.Helper()
	labs, err := content.Load(writeK3sLab(t))
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	lab := labs[0]
	resolved, err := lab.ResolveFor(1)
	if err != nil {
		t.Fatalf("resolve params: %v", err)
	}
	setup, err := os.ReadFile(filepath.Join(lab.Dir, lab.Setup))
	if err != nil {
		t.Fatalf("read setup script: %v", err)
	}
	check, err := os.ReadFile(filepath.Join(lab.Dir, lab.Checkpoints[0].Script))
	if err != nil {
		t.Fatalf("read check script: %v", err)
	}
	spec, err := runner.SpecFromLab(attempt, testImage, lab, resolved, setup)
	if err != nil {
		t.Fatalf("SpecFromLab: %v", err)
	}
	return spec, check
}

func TestK3sSandbox(t *testing.T) {
	p, cli := newProvider(t)
	attempt := attemptID(t)
	spec, check := k3sSpec(t, attempt)

	var mu sync.Mutex
	var steps []string
	spec.Progress = func(step string) {
		mu.Lock()
		defer mu.Unlock()
		steps = append(steps, step)
	}
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
	provisioning := time.Since(start)
	t.Logf("k3s provisioning took %s", provisioning.Round(time.Millisecond))

	if !slices.Contains(steps, "k3s") {
		t.Fatalf("progress steps = %v, want a k3s step", steps)
	}
	if slices.Index(steps, "k3s") > slices.Index(steps, "setup") {
		t.Errorf("progress steps = %v, want k3s before setup", steps)
	}
	if provisioning > 60*time.Second {
		t.Errorf("provisioning took %s, want at most 60s", provisioning.Round(time.Millisecond))
	}

	nodes := string(mustExec(t, p, sb, "k3s01", "kubectl", "get", "nodes", "--no-headers").Stdout)
	if !nodesReady(nodes, []string{"k3s01"}) {
		t.Errorf("kubectl get nodes = %q, want k3s01 Ready", nodes)
	}

	traefik, err := p.Exec(t.Context(), sb, "k3s01", []string{"kubectl", "-n", "kube-system", "get", "deploy", "traefik"}, runner.ExecOptions{Timeout: kubectlTimeout})
	if err != nil {
		t.Fatalf("exec kubectl get deploy traefik: %v", err)
	}
	if traefik.ExitCode == 0 {
		t.Errorf("traefik is deployed although the default disable list should have dropped it: %s", traefik.Stdout)
	}

	pods := waitForCrashLoop(t, p, sb)
	t.Logf("broken pod is failing: %s", strings.TrimSpace(pods))

	if code := runCheck(t, p, sb, spec, check); code == 0 {
		t.Error("check passed although the pod of broken is crashlooping")
	}

	mustExec(t, p, sb, "k3s01", "kubectl", "-n", "nsl", "patch", "deploy", "broken", "--type=json",
		`-p=[{"op":"replace","path":"/spec/template/spec/containers/0/command","value":["sleep","3600"]}]`)

	fixed := time.Now()
	deadline := fixed.Add(60 * time.Second)
	for {
		if runCheck(t, p, sb, spec, check) == 0 {
			t.Logf("check passed %s after the fix", time.Since(fixed).Round(time.Millisecond))
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("check still failing 60s after the fix\n%s", mustExec(t, p, sb, "k3s01", "kubectl", "-n", "nsl", "get", "pods").Stdout)
		}
		time.Sleep(time.Second)
	}

	volume := k3sVolume(t, cli, attempt)
	if err := p.Destroy(t.Context(), runner.SandboxID(attempt)); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	assertNothingLeft(t, cli, attemptFilter(attempt))
	if _, err := cli.VolumeInspect(t.Context(), volume); !cerrdefs.IsNotFound(err) {
		t.Errorf("volume %s survived destroy: %v", volume, err)
	}
}

func runCheck(t *testing.T, p *Provider, sb runner.SandboxID, spec runner.SandboxSpec, check []byte) int {
	t.Helper()
	opts := runner.ExecOptions{Env: spec.Env, Stdin: strings.NewReader(string(check)), Timeout: kubectlTimeout}
	result, err := p.Exec(t.Context(), sb, "k3s01", []string{"bash", "-s"}, opts)
	if err != nil {
		t.Fatalf("exec check: %v", err)
	}
	return result.ExitCode
}

const podStatusTemplate = `{range .items[*]}{.metadata.name} ` +
	`{.status.containerStatuses[*].state.waiting.reason} ` +
	`{.status.containerStatuses[*].restartCount}{"\n"}{end}`

// waitForCrashLoop waits for evidence that the deployment is really broken, so that a
// check failing at an arbitrary moment of the initial rollout cannot pass for it (#48).
func waitForCrashLoop(t *testing.T, p *Provider, sb runner.SandboxID) string {
	t.Helper()
	var out string
	deadline := time.Now().Add(60 * time.Second)
	for {
		out = string(mustExec(t, p, sb, "k3s01", "kubectl", "-n", "nsl", "get", "pods",
			"-o", "jsonpath="+podStatusTemplate).Stdout)
		if crashLooping(out) {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatalf("no pod of broken reached CrashLoopBackOff or restarted within 60s:\n%s", out)
		}
		time.Sleep(time.Second)
	}
}

func crashLooping(status string) bool {
	for line := range strings.Lines(status) {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.HasPrefix(fields[0], "broken-") {
			continue
		}
		if slices.Contains(fields[1:], "CrashLoopBackOff") {
			return true
		}
		if restarts, err := strconv.Atoi(fields[len(fields)-1]); err == nil && restarts >= 1 {
			return true
		}
	}
	return false
}

func k3sVolume(t *testing.T, cli client.APIClient, attempt string) string {
	t.Helper()
	inspected, err := cli.ContainerInspect(t.Context(), containerName(attempt, "k3s01"))
	if err != nil {
		t.Fatalf("inspect k3s01: %v", err)
	}
	for _, m := range inspected.Mounts {
		if m.Type == mount.TypeVolume && m.Destination == k3sContainerdDir {
			return m.Name
		}
	}
	t.Fatalf("k3s01 has no volume on %s: %+v", k3sContainerdDir, inspected.Mounts)
	return ""
}
