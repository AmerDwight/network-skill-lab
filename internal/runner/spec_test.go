package runner

import (
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
