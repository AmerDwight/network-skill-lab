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
	if ticket != "Users report web01 cannot reach db01 (10.0.5.20). Find the cause and fix it." {
		t.Errorf("ticket = %q", ticket)
	}

	topology, err := lab.Topology.Resolve(params)
	if err != nil {
		t.Fatalf("Topology.Resolve() error = %v", err)
	}
	if topology.Links[0].Addresses["web01"] != "10.0.5.10/24" {
		t.Errorf("web01 address = %q", topology.Links[0].Addresses["web01"])
	}
}

func TestLoadMissingDirectory(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nonexistent")); err == nil {
		t.Fatal("Load() succeeded, want error")
	}
}
