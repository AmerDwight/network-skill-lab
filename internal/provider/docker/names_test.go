package docker

import (
	"slices"
	"strings"
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

	node := nodeLabels("att1", "web01", "ubuntu")
	if node[labelManaged] != "true" || node[labelAttempt] != "att1" || node[labelNode] != "web01" || node[labelRole] != "ubuntu" {
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

func TestExecEnv(t *testing.T) {
	env := map[string]string{"NSL_NODE": "k3s01"}
	got := execEnv(runner.RoleK3sServer, env)
	if got["KUBECONFIG"] != kubeconfigPath || got["NSL_NODE"] != "k3s01" {
		t.Errorf("execEnv = %v", got)
	}
	if _, ok := env["KUBECONFIG"]; ok {
		t.Errorf("execEnv modified its input: %v", env)
	}
	if got := execEnv(runner.RoleK3sServer, nil); got["KUBECONFIG"] != kubeconfigPath {
		t.Errorf("execEnv with a nil env = %v", got)
	}
	for _, role := range []string{"k3s-agent", "ubuntu", ""} {
		if got := execEnv(role, env); got["KUBECONFIG"] != "" {
			t.Errorf("execEnv for role %q = %v, want no KUBECONFIG", role, got)
		}
	}
}

func TestBootstrapEnv(t *testing.T) {
	spec := runner.SandboxSpec{
		Nodes: []runner.NodeSpec{
			{Name: "k3s01", Role: runner.RoleK3sServer, K3sDisable: []string{"traefik", "metrics-server"}},
			{Name: "k3s02", Role: "k3s-agent"},
			{Name: "web01", Role: "ubuntu"},
		},
		Links: []runner.LinkSpec{{Name: "l1", Endpoints: []runner.EndpointSpec{
			{Node: "k3s01", Iface: "eth1", Address: "10.0.8.10/24"},
			{Node: "k3s02", Iface: "eth1", Address: "10.0.8.20/24"},
		}}},
	}

	server := bootstrapEnv(spec, spec.Nodes[0], "tok", "10.0.8.10")
	if server["NSL_K3S_DISABLE"] != "traefik,metrics-server" || server["NSL_K3S_TOKEN"] != "tok" {
		t.Errorf("server env = %v", server)
	}
	if server["NSL_IFACES"] != "eth1=10.0.8.10/24" {
		t.Errorf("server ifaces = %q", server["NSL_IFACES"])
	}
	agent := bootstrapEnv(spec, spec.Nodes[1], "tok", "10.0.8.10")
	if _, ok := agent["NSL_K3S_DISABLE"]; ok {
		t.Errorf("agent env = %v, want no NSL_K3S_DISABLE", agent)
	}
	if agent["NSL_K3S_SERVER"] != "10.0.8.10" {
		t.Errorf("agent server = %q", agent["NSL_K3S_SERVER"])
	}
	plain := bootstrapEnv(spec, spec.Nodes[2], "tok", "10.0.8.10")
	if _, ok := plain["NSL_K3S_TOKEN"]; ok {
		t.Errorf("ubuntu env = %v, want no k3s variables", plain)
	}

	empty := bootstrapEnv(spec, runner.NodeSpec{Name: "k3s03", Role: runner.RoleK3sServer}, "tok", "")
	if empty["NSL_K3S_DISABLE"] != "" {
		t.Errorf("NSL_K3S_DISABLE = %q, want empty", empty["NSL_K3S_DISABLE"])
	}
}

func TestNodesReady(t *testing.T) {
	const ready = "k3s01   Ready    control-plane,master   20s   v1.36.4+k3s1\nk3s02   Ready    <none>                 8s    v1.36.4+k3s1\n"
	tests := []struct {
		name   string
		output string
		names  []string
		want   bool
	}{
		{"all ready", ready, []string{"k3s01", "k3s02"}, true},
		{"one not ready", strings.Replace(ready, "k3s02   Ready", "k3s02   NotReady", 1), []string{"k3s01", "k3s02"}, false},
		{"node missing", ready, []string{"k3s01", "k3s03"}, false},
		{"no output", "", []string{"k3s01"}, false},
		{"kubectl error", "The connection to the server localhost:8080 was refused", []string{"k3s01"}, false},
		{"subset", ready, []string{"k3s01"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nodesReady(tt.output, tt.names); got != tt.want {
				t.Errorf("nodesReady = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestK3sNodeNames(t *testing.T) {
	spec := runner.SandboxSpec{Nodes: []runner.NodeSpec{
		{Name: "web01", Role: "ubuntu"},
		{Name: "k3s02", Role: "k3s-agent"},
		{Name: "k3s01", Role: runner.RoleK3sServer},
	}}
	if got := k3sNodeNames(spec); !slices.Equal(got, []string{"k3s02", "k3s01"}) {
		t.Errorf("k3sNodeNames = %v", got)
	}
	if got := k3sServerNode(spec); got != "k3s01" {
		t.Errorf("k3sServerNode = %q", got)
	}
	plain := runner.SandboxSpec{Nodes: []runner.NodeSpec{{Name: "web01", Role: "ubuntu"}}}
	if got := k3sNodeNames(plain); got != nil {
		t.Errorf("k3sNodeNames without k3s nodes = %v", got)
	}
	if got := k3sServerNode(plain); got != "" {
		t.Errorf("k3sServerNode without a server = %q", got)
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
