package docker

import (
	"slices"
	"testing"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
)

func TestNames(t *testing.T) {
	if got, want := containerName("att1", "web01"), "nsl-att1-web01"; got != want {
		t.Errorf("containerName = %q, want %q", got, want)
	}
	if got, want := mgmtNetworkName("att1"), "nsl-att1-mgmt"; got != want {
		t.Errorf("mgmtNetworkName = %q, want %q", got, want)
	}
	if got, want := linkNetworkName("att1", "l2"), "nsl-att1-l2"; got != want {
		t.Errorf("linkNetworkName = %q, want %q", got, want)
	}
}

func TestLabels(t *testing.T) {
	attempt := attemptLabels("att1")
	if attempt[labelManaged] != "true" || attempt[labelAttempt] != "att1" {
		t.Errorf("attemptLabels = %v", attempt)
	}
	if _, ok := attempt[labelNode]; ok {
		t.Errorf("attemptLabels must not carry a node label: %v", attempt)
	}

	node := nodeLabels("att1", "web01")
	if node[labelManaged] != "true" || node[labelAttempt] != "att1" || node[labelNode] != "web01" {
		t.Errorf("nodeLabels = %v", node)
	}
}

func TestFilters(t *testing.T) {
	if got := attemptFilter("att1").Get(labelFilter); !slices.Equal(got, []string{"nsl.attempt=att1"}) {
		t.Errorf("attemptFilter = %v", got)
	}
	if got := managedFilter().Get(labelFilter); !slices.Equal(got, []string{"nsl.managed=true"}) {
		t.Errorf("managedFilter = %v", got)
	}
}

func TestEnvSlice(t *testing.T) {
	got := envSlice(map[string]string{"NSL_NODE": "web01", "NSL_IFACES": "eth1=10.0.5.10/24"})
	want := []string{"NSL_IFACES=eth1=10.0.5.10/24", "NSL_NODE=web01"}
	if !slices.Equal(got, want) {
		t.Errorf("envSlice = %v, want %v", got, want)
	}
}

func TestStderrTail(t *testing.T) {
	var stderr []byte
	for i := range 25 {
		stderr = append(stderr, byte('a'+i), '\n')
	}
	got := stderrTail(stderr)
	if want := 20; len(got) != want*2-1 {
		t.Errorf("stderrTail kept %d bytes, want %d lines", len(got), want)
	}
	if got[:1] != "f" {
		t.Errorf("stderrTail = %q, want it to start at the 6th line", got)
	}
}

func TestK3sServerAddress(t *testing.T) {
	spec := runner.SandboxSpec{
		Nodes: []runner.NodeSpec{{Name: "k3s02", Role: "k3s-agent"}, {Name: "k3s01", Role: "k3s-server"}},
		Links: []runner.LinkSpec{{Name: "l1", Endpoints: []runner.EndpointSpec{
			{Node: "k3s01", Iface: "eth1", Address: "10.0.8.10/24"},
			{Node: "k3s02", Iface: "eth1", Address: "10.0.8.20/24"},
		}}},
	}
	if got, want := k3sServerAddress(spec), "10.0.8.10"; got != want {
		t.Errorf("k3sServerAddress = %q, want %q", got, want)
	}
	if got := k3sServerAddress(runner.SandboxSpec{}); got != "" {
		t.Errorf("k3sServerAddress without a server = %q, want empty", got)
	}
}
