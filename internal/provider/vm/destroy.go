package vm

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
)

type inventory struct {
	domains  []string
	networks []string
}

func (i inventory) empty() bool {
	return len(i.domains) == 0 && len(i.networks) == 0
}

func (p *Provider) owned(ctx context.Context, match func(owner) bool) (inventory, error) {
	var inv inventory
	domains, err := p.listNames(ctx, "list", "--all", "--name")
	if err != nil {
		return inv, fmt.Errorf("list domains: %w", err)
	}
	for _, name := range domains {
		data, err := p.virsh.run(ctx, "dumpxml", name)
		if err != nil {
			if isDomainNotFound(err) {
				continue
			}
			return inv, fmt.Errorf("dumpxml %s: %w", name, err)
		}
		if o, ok := parseOwner(data); ok && match(o) {
			inv.domains = append(inv.domains, name)
		}
	}
	networks, err := p.listNames(ctx, "net-list", "--all", "--name")
	if err != nil {
		return inv, fmt.Errorf("list networks: %w", err)
	}
	for _, name := range networks {
		data, err := p.virsh.run(ctx, "net-dumpxml", name)
		if err != nil {
			if isNetworkNotFound(err) {
				continue
			}
			return inv, fmt.Errorf("net-dumpxml %s: %w", name, err)
		}
		if o, ok := parseOwner(data); ok && match(o) {
			inv.networks = append(inv.networks, name)
		}
	}
	return inv, nil
}

func (p *Provider) listNames(ctx context.Context, args ...string) ([]string, error) {
	out, err := p.virsh.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var names []string
	for line := range strings.Lines(string(out)) {
		if name := strings.TrimSpace(line); strings.HasPrefix(name, namePrefix) {
			names = append(names, name)
		}
	}
	return names, nil
}

func (p *Provider) sandboxMatch(attempt string) func(owner) bool {
	return func(o owner) bool { return o.Instance == p.cfg.Instance && o.Attempt == attempt }
}

func (p *Provider) instanceMatch(o owner) bool {
	return o.Instance == p.cfg.Instance
}

func (p *Provider) remove(ctx context.Context, inv inventory) error {
	var errs []error
	for _, name := range inv.domains {
		if _, err := p.virsh.run(ctx, "destroy", name); err != nil && !isNotRunning(err) && !isDomainNotFound(err) {
			errs = append(errs, fmt.Errorf("destroy domain %s: %w", name, err))
			continue
		}
		if _, err := p.virsh.run(ctx, "undefine", name); err != nil && !isDomainNotFound(err) {
			errs = append(errs, fmt.Errorf("undefine domain %s: %w", name, err))
		}
	}
	for _, name := range inv.networks {
		if _, err := p.virsh.run(ctx, "net-destroy", name); err != nil && !isNetworkNotFound(err) {
			errs = append(errs, fmt.Errorf("destroy network %s: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

func (p *Provider) Destroy(ctx context.Context, sb runner.SandboxID) error {
	attempt := string(sb)
	inv, err := p.owned(ctx, p.sandboxMatch(attempt))
	if err != nil {
		return fmt.Errorf("destroy sandbox %s: %w", sb, err)
	}
	if err := p.remove(ctx, inv); err != nil {
		return fmt.Errorf("destroy sandbox %s: %w", sb, err)
	}
	dir := sandboxDir(p.cfg.Dir, p.cfg.Instance, attempt)
	_, statErr := os.Stat(dir)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("destroy sandbox %s: remove %s: %w", sb, dir, err)
	}
	if inv.empty() && errors.Is(statErr, fs.ErrNotExist) {
		return fmt.Errorf("sandbox %s: %w", sb, runner.ErrSandboxNotFound)
	}
	p.logFor(attempt).Info("destroyed sandbox", "domains", len(inv.domains), "networks", len(inv.networks))
	return nil
}

func (p *Provider) GC(ctx context.Context) error {
	inv, err := p.owned(ctx, p.instanceMatch)
	if err != nil {
		return fmt.Errorf("gc: %w", err)
	}
	errs := []error{p.remove(ctx, inv)}
	dir := instanceDir(p.cfg.Dir, p.cfg.Instance)
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, fmt.Errorf("read %s: %w", dir, err))
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			errs = append(errs, fmt.Errorf("remove %s: %w", entry.Name(), err))
		}
	}
	if err := os.Remove(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, fmt.Errorf("remove %s: %w", dir, err))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("gc: %w", err)
	}
	p.log.Info("collected vm sandboxes", "instance", p.cfg.Instance, "domains", len(inv.domains), "networks", len(inv.networks), "dirs", len(entries))
	return nil
}
