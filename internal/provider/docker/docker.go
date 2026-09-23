package docker

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

type Options struct {
	Image    string
	MemLimit int64
}

type Provider struct {
	cli  client.APIClient
	opts Options
}

func New(cli client.APIClient, opts Options) *Provider {
	if opts.Image == "" {
		opts.Image = "nsl/node"
	}
	return &Provider{cli: cli, opts: opts}
}

var _ runner.Runner = (*Provider)(nil)

func (p *Provider) Capabilities() []runner.Env {
	return []runner.Env{runner.EnvContainer}
}

func (p *Provider) log(attempt string) *slog.Logger {
	return slog.Default().With("attempt", attempt)
}

func (p *Provider) Destroy(ctx context.Context, sb runner.SandboxID) error {
	log := p.log(string(sb))
	if err := p.remove(ctx, attemptFilter(string(sb)), log); err != nil {
		return fmt.Errorf("destroy sandbox %s: %w", sb, err)
	}
	return nil
}

func (p *Provider) GC(ctx context.Context) error {
	if err := p.remove(ctx, managedFilter(), slog.Default()); err != nil {
		return fmt.Errorf("gc: %w", err)
	}
	return nil
}

func (p *Provider) remove(ctx context.Context, f filters.Args, log *slog.Logger) error {
	containers, err := p.cli.ContainerList(ctx, container.ListOptions{All: true, Filters: f})
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}
	var errs []error
	for _, c := range containers {
		opts := container.RemoveOptions{Force: true, RemoveVolumes: true}
		if err := p.cli.ContainerRemove(ctx, c.ID, opts); err != nil && !cerrdefs.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("remove container %s: %w", c.ID, err))
			continue
		}
		log.Debug("removed container", "container", c.ID)
	}

	networks, err := p.cli.NetworkList(ctx, network.ListOptions{Filters: f})
	if err != nil {
		return errors.Join(append(errs, fmt.Errorf("list networks: %w", err))...)
	}
	for _, n := range networks {
		if err := p.cli.NetworkRemove(ctx, n.ID); err != nil && !cerrdefs.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("remove network %s: %w", n.ID, err))
			continue
		}
		log.Debug("removed network", "network", n.ID)
	}
	return errors.Join(errs...)
}

func (p *Provider) Health(ctx context.Context) runner.Health {
	health := runner.Health{ImageName: p.opts.Image, MemAvailableMB: memAvailableMB()}
	if _, err := p.cli.Ping(ctx); err != nil {
		health.Error = fmt.Sprintf("ping docker: %v", err)
		return health
	}
	health.Docker = true

	if _, err := p.cli.ImageInspect(ctx, p.opts.Image); err != nil {
		health.Error = fmt.Sprintf("inspect image %s: %v", p.opts.Image, err)
		return health
	}
	health.Image = true
	health.OK = true
	return health
}

func memAvailableMB() int {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		value, ok := strings.CutPrefix(scanner.Text(), "MemAvailable:")
		if !ok {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) == 0 {
			return 0
		}
		kb, err := strconv.Atoi(fields[0])
		if err != nil {
			return 0
		}
		return kb / 1024
	}
	return 0
}
