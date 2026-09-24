package content

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestLoadRealContent(t *testing.T) {
	labs, err := Load(filepath.Join("..", "..", "content"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(labs) == 0 {
		t.Fatal("Load() returned no labs")
	}
}

func TestLoadTestdata(t *testing.T) {
	labs, err := Load("testdata")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(labs) != 1 {
		t.Fatalf("Load() returned %d labs, want 1", len(labs))
	}

	lab := labs[0]
	if lab.Id != "net-ip-01-link-down" {
		t.Errorf("Id = %q", lab.Id)
	}
	if lab.Title.Get("en") != "Server lost connectivity" {
		t.Errorf("Title.Get(en) = %q", lab.Title.Get("en"))
	}
	if !slices.Equal(lab.Modes, []string{"guided", "real"}) {
		t.Errorf("Modes = %v", lab.Modes)
	}
	if lab.Precheck != nil {
		t.Errorf("Precheck = %+v, want none", lab.Precheck)
	}
	if len(lab.Cases) != 0 {
		t.Errorf("Cases = %v, want none", lab.Cases)
	}

	ids := make([]string, 0, len(lab.Checkpoints))
	for _, cp := range lab.Checkpoints {
		ids = append(ids, cp.Id)
	}
	if want := []string{"link-up", "ping-peer"}; !slices.Equal(ids, want) {
		t.Errorf("checkpoints = %v, want %v", ids, want)
	}

	names := make([]string, 0, len(lab.Params))
	for _, param := range lab.Params {
		names = append(names, param.Name)
	}
	want := []string{"node_a", "node_b", "subnet", "ip_a", "ip_b", "iface"}
	if !slices.Equal(names, want) {
		t.Errorf("param order = %v, want %v", names, want)
	}

	params, err := lab.Params.Resolve(1)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	ticket, err := Render(lab.Ticket.Get("en"), params)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if want := "Users report web01 cannot reach db01 (" + params["ip_b"] + "). Find the cause and fix it."; ticket != want {
		t.Errorf("ticket = %q, want %q", ticket, want)
	}

	topology, err := lab.Topology.Resolve(params)
	if err != nil {
		t.Fatalf("Topology.Resolve() error = %v", err)
	}
	if want := params["ip_a"] + "/24"; topology.Links[0].Addresses["web01"] != want {
		t.Errorf("web01 address = %q, want %q", topology.Links[0].Addresses["web01"], want)
	}
	if topology.Links[0].Subnet != params["subnet"] {
		t.Errorf("subnet = %q, want %q", topology.Links[0].Subnet, params["subnet"])
	}
}

func TestLoadMissingDirectory(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nonexistent")); err == nil {
		t.Fatal("Load() succeeded, want error")
	}
}
