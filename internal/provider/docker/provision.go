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
	bootstrapPath       = "/usr/local/sbin/nsl-bootstrap"
	k3sContainerdDir    = "/var/lib/rancher/k3s/agent/containerd"
	systemdTimeout      = 30 * time.Second
	systemdPollInterval = 250 * time.Millisecond
	bootstrapTimeout    = 2 * time.Minute
	setupTimeout        = 60 * time.Second
	stderrTailLines     = 20
)

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
	progress("setup")
	if err := p.runSetup(ctx, spec, log); err != nil {
		return fmt.Errorf("run setup: %w", err)
	}
	return nil
}

func (p *Provider) createNetworks(ctx context.Context, spec runner.SandboxSpec, log *slog.Logger) error {
	mgmt := mgmtNetworkName(spec.AttemptID)
	if _, err := p.cli.NetworkCreate(ctx, mgmt, network.CreateOptions{Labels: attemptLabels(spec.AttemptID)}); err != nil {
		return fmt.Errorf("network %s: %w", mgmt, err)
	}
	log.Debug("created network", "network", mgmt)

	for _, link := range spec.Links {
		name := linkNetworkName(spec.AttemptID, link.Name)
		opts := network.CreateOptions{
			Internal: true,
			IPAM:     &network.IPAM{Config: []network.IPAMConfig{{Subnet: link.Subnet}}},
			Labels:   attemptLabels(spec.AttemptID),
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
			Labels:     nodeLabels(spec.AttemptID, node.Name),
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
			settings := &network.EndpointSettings{IPAMConfig: &network.EndpointIPAMConfig{IPv4Address: address}}
			if err := p.cli.NetworkConnect(ctx, netName, containerName(spec.AttemptID, endpoint.Node), settings); err != nil {
				return fmt.Errorf("connect %s to %s: %w", endpoint.Node, netName, err)
			}
			log.Debug("connected node", "node", endpoint.Node, "network", netName, "iface", endpoint.Iface, "address", address)
		}
	}
	return nil
}

func (p *Provider) waitSystemd(ctx context.Context, spec runner.SandboxSpec, log *slog.Logger) error {
	return forEachNode(spec.Nodes, func(node runner.NodeSpec) error {
		name := containerName(spec.AttemptID, node.Name)
		deadline := time.Now().Add(systemdTimeout)
		for {
			result, err := p.exec(ctx, name, []string{"systemctl", "is-system-running"}, runner.ExecOptions{})
			if err == nil {
				switch strings.TrimSpace(string(result.Stdout)) {
				case "running", "degraded":
					log.Debug("systemd ready", "node", node.Name)
					return nil
				}
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("node %s: systemd not ready within %s", node.Name, systemdTimeout)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(systemdPollInterval):
			}
		}
	})
}

func (p *Provider) bootstrap(ctx context.Context, spec runner.SandboxSpec, log *slog.Logger) error {
	token, err := randomToken()
	if err != nil {
		return err
	}
	server := k3sServerAddress(spec)

	return forEachNode(spec.Nodes, func(node runner.NodeSpec) error {
		env := map[string]string{
			"NSL_NODE":   node.Name,
			"NSL_ROLE":   node.Role,
			"NSL_IFACES": spec.IfacesEnv(node.Name),
		}
		if isK3sRole(node.Role) {
			env["NSL_K3S_TOKEN"] = token
			env["NSL_K3S_SERVER"] = server
		}
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

		opts := runner.ExecOptions{Env: env, Stdin: bytes.NewReader(spec.Setup.Content), Timeout: setupTimeout}
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
	return role == "k3s-server" || role == "k3s-agent"
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
