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
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

const (
	defaultInstance       = "default"
	networkRemoveAttempts = 15
	activeEndpoints       = "has active endpoints"
	removalInProgress     = "is already in progress"
)

var (
	networkRemoveBackoff = time.Second
	containerGoneTimeout = 15 * time.Second
	containerGonePoll    = 100 * time.Millisecond
)

type Options struct {
	Image          string
	Instance       string
	MemLimit       int64
	SystemdTimeout time.Duration
}

type Provider struct {
	cli  client.APIClient
	opts Options
}

func New(cli client.APIClient, opts Options) *Provider {
	if opts.Image == "" {
		opts.Image = "nsl/node"
	}
	if opts.Instance == "" {
		opts.Instance = defaultInstance
	}
	if opts.SystemdTimeout <= 0 {
		opts.SystemdTimeout = defaultSystemdTimeout
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

type Removed struct {
	Containers int
	Networks   int
}

func (p *Provider) Destroy(ctx context.Context, sb runner.SandboxID) error {
	log := p.log(string(sb))
	if _, err := p.remove(ctx, attemptFilter(string(sb)), log); err != nil {
		return fmt.Errorf("destroy sandbox %s: %w", sb, err)
	}
	return nil
}

func (p *Provider) GC(ctx context.Context) error {
	_, err := p.Collect(ctx)
	return err
}

func (p *Provider) Collect(ctx context.Context) (Removed, error) {
	log := slog.Default().With("instance", p.opts.Instance)
	removed, err := p.remove(ctx, instanceFilter(p.opts.Instance), log)
	if err != nil {
		return removed, fmt.Errorf("gc: %w", err)
	}
	return removed, nil
}

func (p *Provider) remove(ctx context.Context, f filters.Args, log *slog.Logger) (Removed, error) {
	var removed Removed
	containers, err := p.cli.ContainerList(ctx, container.ListOptions{All: true, Filters: f})
	if err != nil {
		return removed, fmt.Errorf("list containers: %w", err)
	}
	var (
		errs     []error
		removing []string
	)
	for _, c := range containers {
		opts := container.RemoveOptions{Force: true, RemoveVolumes: true}
		err := p.cli.ContainerRemove(ctx, c.ID, opts)
		switch {
		case err == nil:
			removed.Containers++
			log.Debug("removed container", "container", c.ID)
		case cerrdefs.IsNotFound(err):
			continue
		case !strings.Contains(err.Error(), removalInProgress):
			errs = append(errs, fmt.Errorf("remove container %s: %w", c.ID, err))
			continue
		}
		removing = append(removing, c.ID)
	}
	if err := p.waitGone(ctx, removing); err != nil {
		if ctx.Err() != nil {
			return removed, errors.Join(append(errs, err)...)
		}
		log.Warn("containers outlived the removal wait", "error", err)
	}

	networks, err := p.cli.NetworkList(ctx, network.ListOptions{Filters: f})
	if err != nil {
		return removed, errors.Join(append(errs, fmt.Errorf("list networks: %w", err))...)
	}
	for _, n := range networks {
		if err := p.removeNetwork(ctx, n.ID); err != nil {
			errs = append(errs, fmt.Errorf("remove network %s: %w", n.ID, err))
			continue
		}
		removed.Networks++
		log.Debug("removed network", "network", n.ID)
	}
	return removed, errors.Join(errs...)
}

// waitGone polls until the daemon no longer knows the containers: a forced
// ContainerRemove can return while the container is still being torn down, and
// its network endpoints stay attached until it is.
func (p *Provider) waitGone(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, containerGoneTimeout)
	defer cancel()
	for _, id := range ids {
		for {
			_, err := p.cli.ContainerInspect(ctx, id)
			if cerrdefs.IsNotFound(err) {
				break
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("container %s still present: %w", id, ctx.Err())
			case <-time.After(containerGonePoll):
			}
		}
	}
	return nil
}

// removeNetwork retries while the daemon still sees endpoints: containers that were
// removed a moment ago are detached asynchronously.
func (p *Provider) removeNetwork(ctx context.Context, id string) error {
	var err error
	for attempt := range networkRemoveAttempts {
		err = p.cli.NetworkRemove(ctx, id)
		if err == nil || cerrdefs.IsNotFound(err) {
			return nil
		}
		if !strings.Contains(err.Error(), activeEndpoints) {
			return err
		}
		if attempt == networkRemoveAttempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(networkRemoveBackoff):
		}
	}
	return err
}

func (p *Provider) Health(ctx context.Context) runner.Health {
	health := runner.Health{ImageName: p.opts.Image, Instance: p.opts.Instance, MemAvailableMB: memAvailableMB()}
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
