package vm

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
)

const (
	bootstrapPath     = "/usr/local/sbin/nsl-bootstrap"
	setupPath         = "/tmp/setup.sh"
	bootstrapTimeout  = 2 * time.Minute
	setupTimeout      = 60 * time.Second
	pingInterval      = 500 * time.Millisecond
	stderrTailLines   = 20
	serialTailLines   = 30
	dirMode           = 0o755
	userKeyFile       = "id_ed25519"
	hostKeyFile       = "host_ed25519.pub"
	cloudInitWaitCmd  = "cloud-init status --wait >/dev/null 2>&1; exit 0"
	bootstrapEnvShell = "set -a; . " + bootstrapEnvPath + "; set +a; exec " + bootstrapPath
)

func (p *Provider) Provision(ctx context.Context, spec runner.SandboxSpec) (runner.SandboxID, error) {
	sb := runner.SandboxID(spec.AttemptID)
	log := p.logFor(spec.AttemptID)
	start := time.Now()
	if err := p.provision(ctx, spec, log); err != nil {
		if cleanupErr := p.Destroy(context.WithoutCancel(ctx), sb); cleanupErr != nil && !errors.Is(cleanupErr, runner.ErrSandboxNotFound) {
			log.Error("cleanup after failed provision", "error", cleanupErr)
		}
		return "", err
	}
	log.Info("provisioned sandbox", "nodes", len(spec.Nodes), "links", len(spec.Links), "duration", time.Since(start).Round(time.Millisecond))
	return sb, nil
}

func (p *Provider) provision(ctx context.Context, spec runner.SandboxSpec, log *slog.Logger) error {
	progress := func(step string) {
		if spec.Progress != nil {
			spec.Progress(step)
		}
	}
	dir := sandboxDir(p.cfg.Dir, p.cfg.Instance, spec.AttemptID)
	if err := makeDir(instanceDir(p.cfg.Dir, p.cfg.Instance)); err != nil {
		return err
	}
	if err := makeDir(dir); err != nil {
		return err
	}

	progress("networks")
	if err := p.createNetworks(ctx, spec, dir, log); err != nil {
		return fmt.Errorf("create networks: %w", err)
	}
	progress("nodes")
	if err := p.createNodes(ctx, spec, dir, log); err != nil {
		return fmt.Errorf("create nodes: %w", err)
	}
	if err := p.waitAgents(ctx, spec, dir, log); err != nil {
		return fmt.Errorf("wait for guest agent: %w", err)
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

func makeDir(dir string) error {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := os.Chmod(dir, dirMode); err != nil {
		return fmt.Errorf("chmod %s: %w", dir, err)
	}
	return nil
}

func (p *Provider) createNetworks(ctx context.Context, spec runner.SandboxSpec, dir string, log *slog.Logger) error {
	for _, link := range spec.Links {
		name := networkName(p.cfg.Instance, spec.AttemptID, link.Name)
		data, err := networkXML(networkParams{Name: name, Owner: owner{Instance: p.cfg.Instance, Attempt: spec.AttemptID, Link: link.Name}})
		if err != nil {
			return err
		}
		path := filepath.Join(dir, "net-"+dirName(link.Name)+".xml")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		if _, err := p.virsh.run(ctx, "net-create", path); err != nil {
			return fmt.Errorf("network %s: %w", name, err)
		}
		log.Debug("created network", "network", name, "subnet", link.Subnet)
	}
	return nil
}

func nodeNetworks(spec runner.SandboxSpec, instance, mgmt, node string) []string {
	networks := []string{mgmt}
	for _, link := range spec.Links {
		for _, endpoint := range link.Endpoints {
			if endpoint.Node == node {
				networks = append(networks, networkName(instance, spec.AttemptID, link.Name))
			}
		}
	}
	return networks
}

func (p *Provider) createNodes(ctx context.Context, spec runner.SandboxSpec, dir string, log *slog.Logger) error {
	userKey, err := newKeyPair("nsl-" + spec.AttemptID)
	if err != nil {
		return err
	}
	// The host key is generated here and handed to cloud-init, so OpenTerminal can pin it
	// without trusting whatever answers on the management network first.
	hostKey, err := newKeyPair("nsl-host-" + spec.AttemptID)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, userKeyFile), userKey.private, 0o600); err != nil {
		return fmt.Errorf("write ssh key: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, hostKeyFile), []byte(hostKey.public+"\n"), 0o644); err != nil {
		return fmt.Errorf("write host key: %w", err)
	}
	k3sToken, err := randomHex(16)
	if err != nil {
		return err
	}

	return forEachNode(spec.Nodes, func(node runner.NodeSpec) error {
		suffix, err := randomHex(4)
		if err != nil {
			return err
		}
		name := domainName(p.cfg.Instance, spec.AttemptID, node.Name)
		s := seed{
			instanceID: name + "-" + suffix,
			hostname:   node.Name,
			userKey:    userKey.public,
			hostKey:    hostKey,
			env:        spec.BootstrapEnv(node, k3sToken),
		}
		if err := p.createNode(ctx, spec, node, name, dir, s); err != nil {
			return fmt.Errorf("node %s: %w", node.Name, err)
		}
		log.Debug("started node", "node", node.Name, "domain", name)
		return nil
	})
}

func (p *Provider) createNode(ctx context.Context, spec runner.SandboxSpec, node runner.NodeSpec, name, dir string, s seed) error {
	base := filepath.Join(dir, dirName(node.Name))
	overlay := base + ".qcow2"
	if _, err := p.command(ctx, "qemu-img", "create", "-q", "-f", "qcow2", "-b", p.cfg.Image, "-F", "qcow2", overlay); err != nil {
		return fmt.Errorf("create overlay: %w", err)
	}

	seedDir := base + "-seed"
	if err := makeDir(seedDir); err != nil {
		return err
	}
	userData, err := s.userData()
	if err != nil {
		return err
	}
	files := map[string][]byte{"user-data": userData, "meta-data": s.metaData(), "network-config": networkConfig()}
	paths := make([]string, 0, len(files))
	for _, file := range []string{"user-data", "meta-data", "network-config"} {
		path := filepath.Join(seedDir, file)
		if err := os.WriteFile(path, files[file], 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		paths = append(paths, path)
	}
	iso := base + "-seed.iso"
	args := append([]string{"-quiet", "-output", iso, "-volid", "cidata", "-joliet", "-rock"}, paths...)
	if _, err := p.command(ctx, "genisoimage", args...); err != nil {
		return fmt.Errorf("create seed iso: %w", err)
	}
	if err := os.Chmod(iso, 0o644); err != nil {
		return fmt.Errorf("chmod seed iso: %w", err)
	}

	data, err := domainXML(domainParams{
		Name:      name,
		Owner:     owner{Instance: p.cfg.Instance, Attempt: spec.AttemptID, Node: node.Name},
		MemoryMiB: p.cfg.MemoryMiB,
		VCPUs:     p.cfg.VCPUs,
		Overlay:   overlay,
		Seed:      iso,
		Networks:  nodeNetworks(spec, p.cfg.Instance, p.cfg.MgmtNetwork, node.Name),
		SerialLog: base + "-serial.log",
	})
	if err != nil {
		return err
	}
	xmlPath := base + "-domain.xml"
	if err := os.WriteFile(xmlPath, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", xmlPath, err)
	}
	if _, err := p.virsh.run(ctx, "define", xmlPath); err != nil {
		return fmt.Errorf("define %s: %w", name, err)
	}
	if _, err := p.virsh.run(ctx, "start", name); err != nil {
		return fmt.Errorf("start %s: %w", name, err)
	}
	return nil
}

func (p *Provider) waitAgents(ctx context.Context, spec runner.SandboxSpec, dir string, log *slog.Logger) error {
	start := time.Now()
	deadline := start.Add(p.cfg.BootTimeout)
	return forEachNode(spec.Nodes, func(node runner.NodeSpec) error {
		name := domainName(p.cfg.Instance, spec.AttemptID, node.Name)
		for {
			err := agentPing(ctx, p.virsh, name)
			if err == nil {
				log.Debug("guest agent ready", "node", node.Name, "duration", time.Since(start).Round(time.Millisecond))
				return nil
			}
			if isDomainNotFound(err) {
				return fmt.Errorf("node %s: %w", node.Name, err)
			}
			if time.Now().After(deadline) {
				serial := filepath.Join(dir, dirName(node.Name)+"-serial.log")
				return fmt.Errorf("node %s: guest agent not ready within %s, last error: %v\nserial log %s:\n%s",
					node.Name, p.cfg.BootTimeout, err, serial, fileTail(serial, serialTailLines))
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(pingInterval):
			}
		}
	})
}

func fileTail(path string, n int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("(unreadable: %v)", err)
	}
	return tail(data, n)
}

func tail(data []byte, n int) string {
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func (p *Provider) bootstrap(ctx context.Context, spec runner.SandboxSpec, log *slog.Logger) error {
	return forEachNode(spec.Nodes, func(node runner.NodeSpec) error {
		name := domainName(p.cfg.Instance, spec.AttemptID, node.Name)
		if _, err := p.exec(ctx, name, []string{"sh", "-c", cloudInitWaitCmd}, runner.ExecOptions{Timeout: p.cfg.BootTimeout}); err != nil {
			return fmt.Errorf("node %s: wait for cloud-init: %w", node.Name, err)
		}
		result, err := p.exec(ctx, name, []string{"sh", "-c", bootstrapEnvShell}, runner.ExecOptions{Timeout: bootstrapTimeout})
		if err != nil {
			return fmt.Errorf("node %s: %w", node.Name, err)
		}
		if result.ExitCode != 0 {
			return scriptError(node.Name, bootstrapPath, result)
		}
		log.Debug("bootstrapped node", "node", node.Name)
		return nil
	})
}

func (p *Provider) runSetup(ctx context.Context, spec runner.SandboxSpec, log *slog.Logger) error {
	if len(spec.Setup.Content) == 0 {
		return nil
	}
	for _, node := range spec.Nodes {
		name := domainName(p.cfg.Instance, spec.AttemptID, node.Name)
		write := runner.ExecOptions{Stdin: bytes.NewReader(spec.Setup.Content), Timeout: setupTimeout}
		result, err := p.exec(ctx, name, []string{"sh", "-c", "cat > " + setupPath}, write)
		if err != nil {
			return fmt.Errorf("node %s: write setup script: %w", node.Name, err)
		}
		if result.ExitCode != 0 {
			return scriptError(node.Name, "write "+setupPath, result)
		}

		env := maps.Clone(spec.Env)
		if env == nil {
			env = map[string]string{}
		}
		env["NSL_NODE"] = node.Name
		result, err = p.exec(ctx, name, []string{"bash", setupPath}, runner.ExecOptions{Env: env, Timeout: setupTimeout})
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
		return fmt.Errorf("node %s: %s timed out\n%s", node, script, tail(result.Stderr, stderrTailLines))
	}
	return fmt.Errorf("node %s: %s exited with %d\n%s", node, script, result.ExitCode, tail(result.Stderr, stderrTailLines))
}

func forEachNode(nodes []runner.NodeSpec, fn func(runner.NodeSpec) error) error {
	errs := make([]error, len(nodes))
	var wg sync.WaitGroup
	for i, node := range nodes {
		wg.Go(func() { errs[i] = fn(node) })
	}
	wg.Wait()
	return errors.Join(errs...)
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
