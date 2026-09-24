package docker

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
)

const (
	bootstrapPath         = "/usr/local/sbin/nsl-bootstrap"
	k3sContainerdDir      = "/var/lib/rancher/k3s/agent/containerd"
	kubeconfigPath        = "/etc/rancher/k3s/k3s.yaml"
	defaultSystemdTimeout = 60 * time.Second
	systemdPollInterval   = 250 * time.Millisecond
	udevTimeout           = 10 * time.Second
	bootstrapTimeout      = 2 * time.Minute
	setupTimeout          = 60 * time.Second
	k3sReadyTimeout       = 90 * time.Second
	k3sPollInterval       = time.Second
	kubectlTimeout        = 15 * time.Second
	stderrTailLines       = 20

	// Link endpoints outrank the mgmt endpoint on a lexicographic tie, so without this
	// the default gateway would move onto a link the moment one is no longer internal.
	linkGwPriority = -1
)

// Link networks must carry traffic routed through a gateway node, so they cannot use
// Internal: true (its DOCKER-INTERNAL rules drop every frame whose source or destination
// sits outside the bridge's own subnet, and with br_netfilter those rules also see frames
// bridged between two containers) nor the default gateway mode (its raw PREROUTING rules
// drop packets addressed to a container that arrive on another bridge). Leaving the bridge
// without an address takes the host's place on the link away instead, so a link subnet is
// still reachable only over the link itself and link traffic is never NATed.
var linkNetworkOptions = map[string]string{
	"com.docker.network.bridge.inhibit_ipv4":         "true",
	"com.docker.network.bridge.gateway_mode_ipv4":    "routed",
	"com.docker.network.bridge.enable_ip_masquerade": "false",
}

func (p *Provider) Provision(ctx context.Context, spec runner.SandboxSpec) (runner.SandboxID, error) {
	sb := runner.SandboxID(spec.AttemptID)
	log := p.log(spec.AttemptID)
	if err := p.provision(ctx, spec, log); err != nil {
		if cleanupErr := p.Destroy(context.WithoutCancel(ctx), sb); cleanupErr != nil {
			log.Error("cleanup after failed provision", "error", cleanupErr)
		}
		return "", err
	}
	log.Info("provisioned sandbox", "nodes", len(spec.Nodes), "links", len(spec.Links))
	return sb, nil
}

func (p *Provider) provision(ctx context.Context, spec runner.SandboxSpec, log *slog.Logger) error {
	progress := func(step string) {
		if spec.Progress != nil {
			spec.Progress(step)
		}
	}

	progress("networks")
	if err := p.createNetworks(ctx, spec, log); err != nil {
		return fmt.Errorf("create networks: %w", err)
	}
	progress("containers")
	if err := p.createContainers(ctx, spec, log); err != nil {
		return fmt.Errorf("create containers: %w", err)
	}
	if err := p.connectLinks(ctx, spec, log); err != nil {
		return fmt.Errorf("connect links: %w", err)
	}
	if err := p.waitSystemd(ctx, spec, log); err != nil {
		return fmt.Errorf("wait for systemd: %w", err)
	}
	progress("bootstrap")
	if err := p.bootstrap(ctx, spec, log); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	if names := k3sNodeNames(spec); len(names) > 0 {
		progress("k3s")
		if err := p.waitK3sReady(ctx, spec, names, log); err != nil {
			return fmt.Errorf("wait for k3s: %w", err)
		}
	}
	progress("setup")
	if err := p.runSetup(ctx, spec, log); err != nil {
		return fmt.Errorf("run setup: %w", err)
	}
	return nil
}

func (p *Provider) createNetworks(ctx context.Context, spec runner.SandboxSpec, log *slog.Logger) error {
	mgmt := mgmtNetworkName(spec.AttemptID)
	mgmtOpts := network.CreateOptions{Internal: !spec.Internet, Labels: attemptLabels(p.opts.Instance, spec.AttemptID)}
	if _, err := p.cli.NetworkCreate(ctx, mgmt, mgmtOpts); err != nil {
		return fmt.Errorf("network %s: %w", mgmt, err)
	}
	log.Debug("created network", "network", mgmt)

	for _, link := range spec.Links {
		name := linkNetworkName(spec.AttemptID, link.Name)
		opts := network.CreateOptions{
			Options: linkNetworkOptions,
			IPAM:    &network.IPAM{Config: []network.IPAMConfig{{Subnet: link.Subnet}}},
			Labels:  attemptLabels(p.opts.Instance, spec.AttemptID),
		}
		if _, err := p.cli.NetworkCreate(ctx, name, opts); err != nil {
			return fmt.Errorf("network %s: %w", name, err)
		}
		log.Debug("created network", "network", name, "subnet", link.Subnet)
	}
	return nil
}

func (p *Provider) createContainers(ctx context.Context, spec runner.SandboxSpec, log *slog.Logger) error {
	image := spec.Image
	if image == "" {
		image = p.opts.Image
	}
	mgmt := mgmtNetworkName(spec.AttemptID)

	for _, node := range spec.Nodes {
		name := containerName(spec.AttemptID, node.Name)
		config := &container.Config{
			Image:      image,
			Hostname:   node.Name,
			Labels:     nodeLabels(p.opts.Instance, spec.AttemptID, node.Name, node.Role),
			StopSignal: "SIGRTMIN+3",
		}
		hostConfig := &container.HostConfig{
			Privileged:   true,
			CgroupnsMode: container.CgroupnsModePrivate,
			Tmpfs:        map[string]string{"/run": "", "/run/lock": "", "/tmp": ""},
			Resources:    container.Resources{Memory: p.opts.MemLimit},
		}
		if isK3sRole(node.Role) {
			hostConfig.Mounts = []mount.Mount{{Type: mount.TypeVolume, Target: k3sContainerdDir}}
		}
		networkConfig := &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{mgmt: {}}}

		created, err := p.cli.ContainerCreate(ctx, config, hostConfig, networkConfig, nil, name)
		if err != nil {
			return fmt.Errorf("container %s: %w", name, err)
		}
		if err := p.cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
			return fmt.Errorf("start container %s: %w", name, err)
		}
		log.Debug("started container", "node", node.Name, "container", created.ID)
	}
	return nil
}

func (p *Provider) connectLinks(ctx context.Context, spec runner.SandboxSpec, log *slog.Logger) error {
	for _, link := range spec.Links {
		netName := linkNetworkName(spec.AttemptID, link.Name)
		for _, endpoint := range link.Endpoints {
			address, _, _ := strings.Cut(endpoint.Address, "/")
			settings := &network.EndpointSettings{
				IPAMConfig: &network.EndpointIPAMConfig{IPv4Address: address},
				GwPriority: linkGwPriority,
			}
			if err := p.cli.NetworkConnect(ctx, netName, containerName(spec.AttemptID, endpoint.Node), settings); err != nil {
				return fmt.Errorf("connect %s to %s: %w", endpoint.Node, netName, err)
			}
			log.Debug("connected node", "node", endpoint.Node, "network", netName, "iface", endpoint.Iface, "address", address)
		}
	}
	return nil
}

func (p *Provider) waitSystemd(ctx context.Context, spec runner.SandboxSpec, log *slog.Logger) error {
	timeout := p.opts.SystemdTimeout
	return forEachNode(spec.Nodes, func(node runner.NodeSpec) error {
		name := containerName(spec.AttemptID, node.Name)
		deadline := time.Now().Add(timeout)
		var state string
		for {
			result, err := p.exec(ctx, name, []string{"systemctl", "is-system-running"}, runner.ExecOptions{})
			if err != nil {
				state = err.Error()
			} else {
				state = strings.TrimSpace(string(result.Stdout))
				switch state {
				case "running":
					log.Debug("systemd ready", "node", node.Name)
					return nil
				case "degraded":
					p.logFailedUnits(ctx, name, node.Name, log)
					p.waitUdev(ctx, name, node.Name, log)
					return nil
				}
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("node %s: systemd not ready within %s, last state %q\npending jobs:\n%s",
					node.Name, timeout, state, p.pendingJobs(ctx, name))
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(systemdPollInterval):
			}
		}
	})
}

func (p *Provider) pendingJobs(ctx context.Context, name string) string {
	result, err := p.exec(ctx, name, []string{"systemctl", "list-jobs", "--no-legend"}, runner.ExecOptions{})
	if err != nil {
		return fmt.Sprintf("list-jobs failed: %v", err)
	}
	if jobs := strings.TrimSpace(string(result.Stdout)); jobs != "" {
		return jobs
	}
	return "none"
}

func (p *Provider) logFailedUnits(ctx context.Context, name, node string, log *slog.Logger) {
	result, err := p.exec(ctx, name, []string{"systemctl", "--failed", "--no-legend", "--plain"}, runner.ExecOptions{})
	if err != nil {
		log.Warn("systemd degraded, listing failed units", "node", node, "error", err)
		return
	}
	log.Warn("systemd degraded", "node", node, "failed", failedUnits(result.Stdout))
}

func failedUnits(stdout []byte) string {
	var units []string
	for line := range strings.SplitSeq(string(stdout), "\n") {
		if fields := strings.Fields(line); len(fields) > 0 {
			units = append(units, strings.Join(fields, " "))
		}
	}
	if len(units) == 0 {
		return "none"
	}
	return strings.Join(units, "; ")
}

func (p *Provider) waitUdev(ctx context.Context, name, node string, log *slog.Logger) {
	deadline := time.Now().Add(udevTimeout)
	for {
		result, err := p.exec(ctx, name, []string{"systemctl", "is-active", "systemd-udevd"}, runner.ExecOptions{})
		if err == nil && isActive(result.Stdout) {
			return
		}
		if time.Now().After(deadline) {
			log.Warn("systemd-udevd not active, bootstrapping anyway", "node", node, "timeout", udevTimeout)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(systemdPollInterval):
		}
	}
}

func isActive(stdout []byte) bool {
	return strings.TrimSpace(string(stdout)) == "active"
}

func (p *Provider) bootstrap(ctx context.Context, spec runner.SandboxSpec, log *slog.Logger) error {
	token, err := randomToken()
	if err != nil {
		return err
	}
	server := k3sServerAddress(spec)

	return forEachNode(spec.Nodes, func(node runner.NodeSpec) error {
		env := bootstrapEnv(spec, node, token, server)
		name := containerName(spec.AttemptID, node.Name)
		result, err := p.exec(ctx, name, []string{bootstrapPath}, runner.ExecOptions{Env: env, Timeout: bootstrapTimeout})
		if err != nil {
			return fmt.Errorf("node %s: %w", node.Name, err)
		}
		if result.ExitCode != 0 {
			return scriptError(node.Name, bootstrapPath, result)
		}
		log.Debug("bootstrapped node", "node", node.Name, "ifaces", env["NSL_IFACES"])
		return nil
	})
}

func bootstrapEnv(spec runner.SandboxSpec, node runner.NodeSpec, token, server string) map[string]string {
	env := map[string]string{
		"NSL_NODE":   node.Name,
		"NSL_ROLE":   node.Role,
		"NSL_IFACES": spec.IfacesEnv(node.Name),
	}
	if isK3sRole(node.Role) {
		env["NSL_K3S_TOKEN"] = token
		env["NSL_K3S_SERVER"] = server
	}
	if node.Role == runner.RoleK3sServer {
		env["NSL_K3S_DISABLE"] = strings.Join(node.K3sDisable, ",")
	}
	return env
}

func (p *Provider) waitK3sReady(ctx context.Context, spec runner.SandboxSpec, names []string, log *slog.Logger) error {
	server := k3sServerNode(spec)
	if server == "" {
		return errors.New("spec has k3s nodes but no k3s-server")
	}
	name := containerName(spec.AttemptID, server)
	opts := runner.ExecOptions{Env: map[string]string{"KUBECONFIG": kubeconfigPath}, Timeout: kubectlTimeout}
	start := time.Now()
	deadline := start.Add(k3sReadyTimeout)

	var last string
	for {
		result, err := p.exec(ctx, name, []string{"kubectl", "get", "nodes", "--no-headers"}, opts)
		if err != nil {
			last = err.Error()
		} else {
			last = strings.TrimSpace(string(result.Stdout) + string(result.Stderr))
			if nodesReady(last, names) {
				log.Info("k3s nodes ready", "nodes", len(names), "duration", time.Since(start).Round(time.Millisecond))
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("nodes not ready within %s\n%s", k3sReadyTimeout, last)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(k3sPollInterval):
		}
	}
}

func nodesReady(output string, names []string) bool {
	status := map[string]string{}
	for line := range strings.Lines(output) {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		status[fields[0]] = fields[1]
	}
	for _, name := range names {
		if status[name] != "Ready" {
			return false
		}
	}
	return true
}

func (p *Provider) runSetup(ctx context.Context, spec runner.SandboxSpec, log *slog.Logger) error {
	if len(spec.Setup.Content) == 0 {
		return nil
	}
	for _, node := range spec.Nodes {
		env := maps.Clone(spec.Env)
		if env == nil {
			env = map[string]string{}
		}
		env["NSL_NODE"] = node.Name

		opts := runner.ExecOptions{Env: execEnv(node.Role, env), Stdin: bytes.NewReader(spec.Setup.Content), Timeout: setupTimeout}
		name := containerName(spec.AttemptID, node.Name)
		result, err := p.exec(ctx, name, []string{"bash", "-s"}, opts)
		if err != nil {
			return fmt.Errorf("node %s: %w", node.Name, err)
		}
		if result.ExitCode != 0 {
			return scriptError(node.Name, spec.Setup.Name, result)
		}
		log.Debug("ran setup", "node", node.Name, "script", spec.Setup.Name)
	}
	return nil
}

func scriptError(node, script string, result runner.ExecResult) error {
	if result.TimedOut {
		return fmt.Errorf("node %s: %s timed out\n%s", node, script, stderrTail(result.Stderr))
	}
	return fmt.Errorf("node %s: %s exited with %d\n%s", node, script, result.ExitCode, stderrTail(result.Stderr))
}

func stderrTail(stderr []byte) string {
	lines := strings.Split(strings.TrimRight(string(stderr), "\n"), "\n")
	if len(lines) > stderrTailLines {
		lines = lines[len(lines)-stderrTailLines:]
	}
	return strings.Join(lines, "\n")
}

func forEachNode(nodes []runner.NodeSpec, fn func(runner.NodeSpec) error) error {
	errs := make([]error, len(nodes))
	var wg sync.WaitGroup
	for i, node := range nodes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = fn(node)
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

func isK3sRole(role string) bool {
	return role == runner.RoleK3sServer || role == "k3s-agent"
}

func k3sNodeNames(spec runner.SandboxSpec) []string {
	var names []string
	for _, node := range spec.Nodes {
		if isK3sRole(node.Role) {
			names = append(names, node.Name)
		}
	}
	return names
}

func k3sServerNode(spec runner.SandboxSpec) string {
	for _, node := range spec.Nodes {
		if node.Role == runner.RoleK3sServer {
			return node.Name
		}
	}
	return ""
}

func k3sServerAddress(spec runner.SandboxSpec) string {
	for _, node := range spec.Nodes {
		if node.Role != "k3s-server" {
			continue
		}
		ifaces := spec.IfacesEnv(node.Name)
		if ifaces == "" {
			return ""
		}
		first, _, _ := strings.Cut(ifaces, ",")
		_, address, _ := strings.Cut(first, "=")
		address, _, _ = strings.Cut(address, "/")
		return address
	}
	return ""
}

func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate k3s token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
