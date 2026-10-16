package runner

import (
	"slices"
	"strings"
	"testing"

	"github.com/AmerDwight/network-skill-lab/internal/content"
)

func testLab(links []content.Link) content.Lab {
	return content.Lab{
		Setup: "setup.sh",
		Topology: content.Topology{
			Nodes: map[string]content.Node{
				"web01": {Role: "ubuntu"},
				"db01":  {Role: "ubuntu"},
				"r1":    {Role: "ubuntu"},
			},
			Links: links,
		},
	}
}

func link(nodeA, ifaceA, addrA, nodeB, ifaceB, addrB, subnet string) content.Link {
	return content.Link{
		Endpoints: [2]content.Endpoint{{Node: nodeA, Iface: ifaceA}, {Node: nodeB, Iface: ifaceB}},
		Subnet:    subnet,
		Addresses: map[string]string{nodeA: addrA, nodeB: addrB},
	}
}

func TestSpecFromLab(t *testing.T) {
	lab := testLab([]content.Link{
		link("web01", "eth1", "10.0.5.10/24", "db01", "eth1", "10.0.5.20/24", "{{subnet}}"),
		link("web01", "eth2", "10.0.6.10/24", "r1", "eth1", "10.0.6.1/24", "10.0.6.0/24"),
	})
	params := map[string]string{"subnet": "10.0.5.0/24"}

	spec, err := SpecFromLab("att1", "nsl/node", lab, content.Resolved{Params: params}, []byte("#!/bin/bash\n"))
	if err != nil {
		t.Fatalf("SpecFromLab: %v", err)
	}

	if spec.AttemptID != "att1" || spec.Image != "nsl/node" {
		t.Errorf("got attempt %q image %q", spec.AttemptID, spec.Image)
	}
	wantNodes := []string{"db01", "r1", "web01"}
	if len(spec.Nodes) != len(wantNodes) {
		t.Fatalf("got %d nodes, want %d", len(spec.Nodes), len(wantNodes))
	}
	for i, name := range wantNodes {
		if spec.Nodes[i].Name != name || spec.Nodes[i].Role != "ubuntu" {
			t.Errorf("node %d = %+v, want %s/ubuntu", i, spec.Nodes[i], name)
		}
	}

	if spec.Links[0].Name != "l1" || spec.Links[1].Name != "l2" {
		t.Errorf("got link names %q, %q", spec.Links[0].Name, spec.Links[1].Name)
	}
	if spec.Links[0].Subnet != "10.0.5.0/24" {
		t.Errorf("subnet was not rendered: %q", spec.Links[0].Subnet)
	}
	if got := spec.Links[0].Endpoints[0]; got != (EndpointSpec{Node: "web01", Iface: "eth1", Address: "10.0.5.10/24"}) {
		t.Errorf("endpoint = %+v", got)
	}
	if spec.Setup.Name != "setup.sh" || string(spec.Setup.Content) != "#!/bin/bash\n" {
		t.Errorf("setup = %+v", spec.Setup)
	}
	if spec.Env["NSL_SUBNET"] != "10.0.5.0/24" {
		t.Errorf("env = %v", spec.Env)
	}
}

func TestSpecFromLabIfaceOrder(t *testing.T) {
	tests := []struct {
		name  string
		links []content.Link
	}{
		{
			name:  "first link must be eth1",
			links: []content.Link{link("web01", "eth2", "10.0.5.10/24", "db01", "eth1", "10.0.5.20/24", "10.0.5.0/24")},
		},
		{
			name: "second link must be eth2",
			links: []content.Link{
				link("web01", "eth1", "10.0.5.10/24", "db01", "eth1", "10.0.5.20/24", "10.0.5.0/24"),
				link("web01", "eth3", "10.0.6.10/24", "r1", "eth1", "10.0.6.1/24", "10.0.6.0/24"),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := SpecFromLab("att1", "nsl/node", testLab(test.links), content.Resolved{}, nil)
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), "web01") {
				t.Errorf("error does not name the node: %v", err)
			}
		})
	}
}

func TestSpecFromLabUnknownParam(t *testing.T) {
	lab := testLab([]content.Link{link("web01", "eth1", "10.0.5.10/24", "db01", "eth1", "{{missing}}", "10.0.5.0/24")})
	if _, err := SpecFromLab("att1", "nsl/node", lab, content.Resolved{}, nil); err == nil {
		t.Fatal("want an error")
	}
}

func TestIfacesEnv(t *testing.T) {
	spec := SandboxSpec{Links: []LinkSpec{
		{Name: "l1", Endpoints: []EndpointSpec{{Node: "web01", Iface: "eth1", Address: "10.0.5.10/24"}, {Node: "db01", Iface: "eth1", Address: "10.0.5.20/24"}}},
		{Name: "l2", Endpoints: []EndpointSpec{{Node: "web01", Iface: "eth2", Address: "10.0.6.10/24"}}},
	}}

	if got, want := spec.IfacesEnv("web01"), "eth1=10.0.5.10/24,eth2=10.0.6.10/24"; got != want {
		t.Errorf("web01 = %q, want %q", got, want)
	}
	if got, want := spec.IfacesEnv("db01"), "eth1=10.0.5.20/24"; got != want {
		t.Errorf("db01 = %q, want %q", got, want)
	}
	if got := spec.IfacesEnv("absent"); got != "" {
		t.Errorf("absent = %q, want empty", got)
	}
}

func TestSpecFromLabInternet(t *testing.T) {
	lab := testLab(nil)
	off := false
	tests := []struct {
		name     string
		internet *bool
		want     bool
	}{
		{"unset defaults to on", nil, true},
		{"explicitly off", &off, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab.Internet = tt.internet
			spec, err := SpecFromLab("att1", "nsl/node", lab, content.Resolved{}, nil)
			if err != nil {
				t.Fatalf("SpecFromLab: %v", err)
			}
			if spec.Internet != tt.want {
				t.Errorf("Internet = %v, want %v", spec.Internet, tt.want)
			}
		})
	}
}

func TestSpecFromLabK3sDisable(t *testing.T) {
	tests := []struct {
		name string
		node content.Node
		want []string
	}{
		{"server without options", content.Node{Role: RoleK3sServer}, []string{"traefik", "metrics-server"}},
		{"server with empty options", content.Node{Role: RoleK3sServer, K3s: &content.K3sOptions{}}, nil},
		{"server disabling nothing", content.Node{Role: RoleK3sServer, K3s: &content.K3sOptions{Disable: []string{}}}, []string{}},
		{"server with an explicit list", content.Node{Role: RoleK3sServer, K3s: &content.K3sOptions{Disable: []string{"metrics-server"}}}, []string{"metrics-server"}},
		{"agent", content.Node{Role: "k3s-agent"}, nil},
		{"ubuntu", content.Node{Role: "ubuntu"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := content.Lab{Topology: content.Topology{Nodes: map[string]content.Node{"k3s01": tt.node}}}
			spec, err := SpecFromLab("att1", "nsl/node", lab, content.Resolved{}, nil)
			if err != nil {
				t.Fatalf("SpecFromLab: %v", err)
			}
			if !slices.Equal(spec.Nodes[0].K3sDisable, tt.want) {
				t.Errorf("K3sDisable = %v, want %v", spec.Nodes[0].K3sDisable, tt.want)
			}
		})
	}
}

func TestSpecFromLabK3sDisableDefaultIsCopied(t *testing.T) {
	lab := content.Lab{Topology: content.Topology{Nodes: map[string]content.Node{"k3s01": {Role: RoleK3sServer}}}}
	spec, err := SpecFromLab("att1", "nsl/node", lab, content.Resolved{}, nil)
	if err != nil {
		t.Fatalf("SpecFromLab: %v", err)
	}
	spec.Nodes[0].K3sDisable[0] = "mutated"
	if defaultK3sDisable[0] != "traefik" {
		t.Errorf("defaultK3sDisable was mutated: %v", defaultK3sDisable)
	}
}

func TestSpecFromLabEnvIncludesSetupEnv(t *testing.T) {
	resolved := content.Resolved{
		Params:   map[string]string{"iface": "eth1"},
		SetupEnv: map[string]string{"NSL_FAULT_MTU": "1200"},
	}
	spec, err := SpecFromLab("att1", "nsl/node", testLab(nil), resolved, nil)
	if err != nil {
		t.Fatalf("SpecFromLab: %v", err)
	}
	if spec.Env["NSL_IFACE"] != "eth1" || spec.Env["NSL_FAULT_MTU"] != "1200" {
		t.Errorf("Env = %v", spec.Env)
	}
}

func TestBootstrapEnv(t *testing.T) {
	spec := SandboxSpec{
		Nodes: []NodeSpec{
			{Name: "k3s01", Role: RoleK3sServer, K3sDisable: []string{"traefik", "metrics-server"}},
			{Name: "k3s02", Role: RoleK3sAgent},
			{Name: "web01", Role: "ubuntu"},
		},
		Links: []LinkSpec{{Name: "l1", Endpoints: []EndpointSpec{
			{Node: "k3s01", Iface: "eth1", Address: "10.0.8.10/24"},
			{Node: "k3s02", Iface: "eth1", Address: "10.0.8.20/24"},
		}}},
	}

	server := spec.BootstrapEnv(spec.Nodes[0], "tok")
	if server["NSL_K3S_DISABLE"] != "traefik,metrics-server" || server["NSL_K3S_TOKEN"] != "tok" {
		t.Errorf("server env = %v", server)
	}
	if server["NSL_IFACES"] != "eth1=10.0.8.10/24" {
		t.Errorf("server ifaces = %q", server["NSL_IFACES"])
	}
	agent := spec.BootstrapEnv(spec.Nodes[1], "tok")
	if _, ok := agent["NSL_K3S_DISABLE"]; ok {
		t.Errorf("agent env = %v, want no NSL_K3S_DISABLE", agent)
	}
	if agent["NSL_K3S_SERVER"] != "10.0.8.10" {
		t.Errorf("agent server = %q", agent["NSL_K3S_SERVER"])
	}
	plain := spec.BootstrapEnv(spec.Nodes[2], "tok")
	if len(plain) != 3 || plain["NSL_NODE"] != "web01" || plain["NSL_ROLE"] != "ubuntu" {
		t.Errorf("ubuntu env = %v, want only NSL_NODE, NSL_ROLE and NSL_IFACES", plain)
	}

	empty := spec.BootstrapEnv(NodeSpec{Name: "k3s03", Role: RoleK3sServer}, "tok")
	if empty["NSL_K3S_DISABLE"] != "" {
		t.Errorf("NSL_K3S_DISABLE = %q, want empty", empty["NSL_K3S_DISABLE"])
	}
}

func TestK3sServerAddress(t *testing.T) {
	spec := SandboxSpec{
		Nodes: []NodeSpec{{Name: "k3s02", Role: RoleK3sAgent}, {Name: "k3s01", Role: RoleK3sServer}},
		Links: []LinkSpec{{Name: "l1", Endpoints: []EndpointSpec{
			{Node: "k3s01", Iface: "eth1", Address: "10.0.8.10/24"},
			{Node: "k3s02", Iface: "eth1", Address: "10.0.8.20/24"},
		}}},
	}
	if got, want := spec.K3sServerAddress(), "10.0.8.10"; got != want {
		t.Errorf("K3sServerAddress = %q, want %q", got, want)
	}
	if got := (SandboxSpec{}).K3sServerAddress(); got != "" {
		t.Errorf("K3sServerAddress without a server = %q, want empty", got)
	}
}
