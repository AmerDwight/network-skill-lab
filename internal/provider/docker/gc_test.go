package docker

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
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
	previousBackoff, previousPoll := networkRemoveBackoff, containerGonePoll
	networkRemoveBackoff, containerGonePoll = time.Millisecond, time.Millisecond
	t.Cleanup(func() { networkRemoveBackoff, containerGonePoll = previousBackoff, previousPoll })
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

type teardownClient struct {
	client.APIClient
	removeErr  error
	lingers    int
	inspects   int
	events     []string
	containers []container.Summary
	networks   []network.Summary
}

func (c *teardownClient) ContainerList(context.Context, container.ListOptions) ([]container.Summary, error) {
	return c.containers, nil
}

func (c *teardownClient) ContainerRemove(_ context.Context, id string, _ container.RemoveOptions) error {
	c.events = append(c.events, "remove "+id)
	return c.removeErr
}

func (c *teardownClient) ContainerInspect(_ context.Context, id string) (container.InspectResponse, error) {
	c.inspects++
	if c.inspects <= c.lingers {
		return container.InspectResponse{}, nil
	}
	c.events = append(c.events, "gone "+id)
	return container.InspectResponse{}, cerrdefs.ErrNotFound
}

func (c *teardownClient) NetworkList(context.Context, network.ListOptions) ([]network.Summary, error) {
	return c.networks, nil
}

func (c *teardownClient) NetworkRemove(_ context.Context, id string) error {
	c.events = append(c.events, "remove "+id)
	return nil
}

func TestDestroyWaitsForContainersBeforeRemovingNetworks(t *testing.T) {
	fastBackoff(t)
	cli := &teardownClient{
		lingers:    3,
		containers: []container.Summary{{ID: "c1"}},
		networks:   []network.Summary{{ID: "net1"}},
	}
	p := New(cli, Options{Instance: "inst1"})

	if err := p.Destroy(t.Context(), "att1"); err != nil {
		t.Fatalf("Destroy() error = %v", err)
	}
	want := []string{"remove c1", "gone c1", "remove net1"}
	if !slices.Equal(cli.events, want) {
		t.Errorf("events = %v, want %v", cli.events, want)
	}
	if cli.inspects != 4 {
		t.Errorf("ContainerInspect called %d times, want 4", cli.inspects)
	}
}

func TestDestroyWaitsOutARemovalAlreadyInProgress(t *testing.T) {
	fastBackoff(t)
	cli := &teardownClient{
		removeErr:  errors.New("Error response from daemon: removal of container c1 is already in progress"),
		lingers:    1,
		containers: []container.Summary{{ID: "c1"}},
		networks:   []network.Summary{{ID: "net1"}},
	}
	p := New(cli, Options{Instance: "inst1"})

	if err := p.Destroy(t.Context(), "att1"); err != nil {
		t.Fatalf("Destroy() error = %v", err)
	}
	want := []string{"remove c1", "gone c1", "remove net1"}
	if !slices.Equal(cli.events, want) {
		t.Errorf("events = %v, want %v", cli.events, want)
	}
}

func TestDestroyGivesUpWaitingAfterTheTimeout(t *testing.T) {
	fastBackoff(t)
	previous := containerGoneTimeout
	containerGoneTimeout = 20 * time.Millisecond
	t.Cleanup(func() { containerGoneTimeout = previous })
	cli := &teardownClient{
		lingers:    1 << 30,
		containers: []container.Summary{{ID: "c1"}},
		networks:   []network.Summary{{ID: "net1"}},
	}
	p := New(cli, Options{Instance: "inst1"})

	if err := p.Destroy(t.Context(), "att1"); err != nil {
		t.Fatalf("Destroy() error = %v", err)
	}
	if want := []string{"remove c1", "remove net1"}; !slices.Equal(cli.events, want) {
		t.Errorf("events = %v, want %v", cli.events, want)
	}
}

func TestCollectCountsWhatItRemoved(t *testing.T) {
	fastBackoff(t)
	cli := &teardownClient{
		containers: []container.Summary{{ID: "c1"}, {ID: "c2"}},
		networks:   []network.Summary{{ID: "net1"}, {ID: "net2"}, {ID: "net3"}},
	}
	p := New(cli, Options{Instance: "inst1"})

	removed, err := p.Collect(t.Context())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if removed != (Removed{Containers: 2, Networks: 3}) {
		t.Errorf("Collect() = %+v, want 2 containers and 3 networks", removed)
	}
}
