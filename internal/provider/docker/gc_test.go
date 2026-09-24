package docker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

type stubClient struct {
	client.APIClient
	networks   []network.Summary
	removals   int
	failsFirst int
}

func (c *stubClient) ContainerList(context.Context, container.ListOptions) ([]container.Summary, error) {
	return nil, nil
}

func (c *stubClient) NetworkList(context.Context, network.ListOptions) ([]network.Summary, error) {
	return c.networks, nil
}

func (c *stubClient) NetworkRemove(context.Context, string) error {
	c.removals++
	if c.removals <= c.failsFirst {
		return errors.New("Error response from daemon: error while removing network: network nsl-att1-mgmt has active endpoints")
	}
	return nil
}

func fastBackoff(t *testing.T) {
	t.Helper()
	previous := networkRemoveBackoff
	networkRemoveBackoff = time.Millisecond
	t.Cleanup(func() { networkRemoveBackoff = previous })
}

func TestGCRetriesWhileANetworkStillHasEndpoints(t *testing.T) {
	fastBackoff(t)
	cli := &stubClient{networks: []network.Summary{{ID: "net1"}}, failsFirst: 2}
	p := New(cli, Options{Instance: "inst1"})

	if err := p.GC(t.Context()); err != nil {
		t.Fatalf("GC() error = %v", err)
	}
	if cli.removals != 3 {
		t.Errorf("NetworkRemove called %d times, want 3", cli.removals)
	}
}

func TestGCGivesUpAfterTheRetryBudget(t *testing.T) {
	fastBackoff(t)
	cli := &stubClient{networks: []network.Summary{{ID: "net1"}}, failsFirst: networkRemoveAttempts}
	p := New(cli, Options{Instance: "inst1"})

	err := p.GC(t.Context())
	if err == nil {
		t.Fatal("GC() error = nil, want the active-endpoints error")
	}
	if cli.removals != networkRemoveAttempts {
		t.Errorf("NetworkRemove called %d times, want %d", cli.removals, networkRemoveAttempts)
	}
}

func TestGCFiltersOnThisInstance(t *testing.T) {
	cli := &recordingClient{}
	p := New(cli, Options{Instance: "inst1"})

	if err := p.GC(t.Context()); err != nil {
		t.Fatalf("GC() error = %v", err)
	}
	for _, f := range []filters.Args{cli.containerFilter, cli.networkFilter} {
		got := f.Get(labelFilter)
		if len(got) != 2 || !f.ExactMatch(labelFilter, labelInstance+"=inst1") || !f.ExactMatch(labelFilter, labelManaged+"=true") {
			t.Errorf("filter = %v, want it scoped to the managed resources of inst1", got)
		}
	}
}

type recordingClient struct {
	client.APIClient
	containerFilter filters.Args
	networkFilter   filters.Args
}

func (c *recordingClient) ContainerList(_ context.Context, opts container.ListOptions) ([]container.Summary, error) {
	c.containerFilter = opts.Filters
	return nil, nil
}

func (c *recordingClient) NetworkList(_ context.Context, opts network.ListOptions) ([]network.Summary, error) {
	c.networkFilter = opts.Filters
	return nil, nil
}
