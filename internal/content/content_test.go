package content

import (
	"maps"
	"strings"
	"testing"
)

func TestLocalizedGet(t *testing.T) {
	tests := []struct {
		name      string
		localized Localized
		lang      string
		want      string
	}{
		{"zh", Localized{Zh: "中文", En: "english"}, "zh", "中文"},
		{"en", Localized{Zh: "中文", En: "english"}, "en", "english"},
		{"unknown language falls back to en", Localized{Zh: "中文", En: "english"}, "ja", "english"},
		{"missing zh falls back to en", Localized{En: "english"}, "zh", "english"},
		{"missing en falls back to zh", Localized{Zh: "中文"}, "en", "中文"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.localized.Get(tt.lang); got != tt.want {
				t.Errorf("Get(%q) = %q, want %q", tt.lang, got, tt.want)
			}
		})
	}
}

func TestRender(t *testing.T) {
	params := map[string]string{"node_a": "web01", "ip_b": "10.0.5.20"}
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr string
	}{
		{name: "no placeholder", in: "plain text", want: "plain text"},
		{name: "single placeholder", in: "{{node_a}}", want: "web01"},
		{name: "several placeholders", in: "{{node_a}} -> {{ip_b}}", want: "web01 -> 10.0.5.20"},
		{name: "whitespace inside braces", in: "{{  node_a }}", want: "web01"},
		{name: "unknown param", in: "{{node_c}}", wantErr: `unknown param "node_c"`},
		{name: "reports every unknown param", in: "{{node_c}} {{node_d}}", wantErr: `unknown param "node_d"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Render(tt.in, params)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Render(%q) succeeded, want error containing %q", tt.in, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Render(%q) error = %v, want it to contain %q", tt.in, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Render(%q) error = %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("Render(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParamsEnv(t *testing.T) {
	got := ParamsEnv(map[string]string{"ip_b": "10.0.5.20", "iface": "eth1"})
	want := map[string]string{"NSL_IP_B": "10.0.5.20", "NSL_IFACE": "eth1"}
	if !maps.Equal(got, want) {
		t.Errorf("ParamsEnv() = %v, want %v", got, want)
	}
}

func TestParamsResolveInOrder(t *testing.T) {
	params := Params{
		{Name: "node_a", Gen: GenConst, Value: "web01"},
		{Name: "host", Gen: GenConst, Value: "{{node_a}}.example"},
	}
	got, err := params.Resolve(1)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got["host"] != "web01.example" {
		t.Errorf("Resolve()[host] = %q, want %q", got["host"], "web01.example")
	}
}

func TestTopologyResolve(t *testing.T) {
	topology := Topology{
		Nodes: map[string]Node{"web01": {Role: "ubuntu"}, "db01": {Role: "ubuntu"}},
		Links: []Link{{
			Endpoints: [2]Endpoint{{Node: "web01", Iface: "eth1"}, {Node: "db01", Iface: "eth1"}},
			Subnet:    "{{subnet}}",
			Addresses: map[string]string{"web01": "{{ip_a}}/24", "db01": "{{ip_b}}/24"},
		}},
	}
	params := map[string]string{"subnet": "10.0.5.0/24", "ip_a": "10.0.5.10", "ip_b": "10.0.5.20"}

	resolved, err := topology.Resolve(params)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Links[0].Subnet != "10.0.5.0/24" {
		t.Errorf("subnet = %q, want %q", resolved.Links[0].Subnet, "10.0.5.0/24")
	}
	if !maps.Equal(resolved.Links[0].Addresses, map[string]string{"web01": "10.0.5.10/24", "db01": "10.0.5.20/24"}) {
		t.Errorf("addresses = %v", resolved.Links[0].Addresses)
	}
	if topology.Links[0].Subnet != "{{subnet}}" {
		t.Error("Resolve() mutated the original topology")
	}

	if _, err := topology.Resolve(map[string]string{}); err == nil {
		t.Error("Resolve() with no params succeeded, want error")
	}
}
