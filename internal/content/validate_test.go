package content

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validLabYAML = `id: net-ip-01-link-down
version: 1
title: { zh: "伺服器連不到對外", en: "Server lost connectivity" }
topic: net/ip
level: 2
modes: [guided]
environment: container
estimated_minutes: 10
ticket:
  zh: "{{node_a}} 無法連到 {{node_b}}（{{ip_b}}）。"
  en: "{{node_a}} cannot reach {{node_b}} ({{ip_b}})."
params:
  node_a: { gen: const, value: web01 }
  node_b: { gen: const, value: db01 }
  subnet: { gen: const, value: 10.0.5.0/24 }
  ip_a:   { gen: const, value: 10.0.5.10 }
  ip_b:   { gen: const, value: 10.0.5.20 }
  iface:  { gen: const, value: eth1 }
setup: setup.sh
checkpoints:
  - id: link-up
    title: { zh: "{{iface}} 已 UP", en: "{{iface}} is up" }
    node: web01
    script: checks/01-link-up.sh
    visible: true
  - id: ping-peer
    title: { zh: "web01 可 ping 到 db01", en: "web01 can ping db01" }
    node: web01
    script: checks/02-ping-peer.sh
    visible: true
solution: { zh: solution.zh.md, en: solution.en.md }
`

const validTopologyYAML = `nodes:
  web01: { role: ubuntu }
  db01:  { role: ubuntu }
links:
  - endpoints: ["web01:eth1", "db01:eth1"]
    subnet: "{{subnet}}"
    addresses: { web01: "{{ip_a}}/24", db01: "{{ip_b}}/24" }
`

type fixtureFile struct {
	data string
	mode fs.FileMode
}

type fixture struct {
	id       string
	lab      string
	topology string
	files    map[string]fixtureFile
}

func newFixture() *fixture {
	return &fixture{
		id:       "net-ip-01-link-down",
		lab:      validLabYAML,
		topology: validTopologyYAML,
		files: map[string]fixtureFile{
			"setup.sh":               {"#!/usr/bin/env bash\n", 0o755},
			"checks/01-link-up.sh":   {"#!/usr/bin/env bash\n", 0o755},
			"checks/02-ping-peer.sh": {"#!/usr/bin/env bash\n", 0o755},
			"solution.zh.md":         {"zh\n", 0o644},
			"solution.en.md":         {"en\n", 0o644},
		},
	}
}

func (f *fixture) replace(field, old, new string) {
	switch field {
	case "lab":
		f.lab = strings.Replace(f.lab, old, new, 1)
	case "topology":
		f.topology = strings.Replace(f.topology, old, new, 1)
	}
}

func (f *fixture) write(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "labs", f.id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(dir, "lab.yaml"), f.lab, 0o644)
	writeFixtureFile(t, filepath.Join(dir, "topology.yaml"), f.topology, 0o644)
	for name, file := range f.files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFixtureFile(t, path, file.data, file.mode)
	}
	return root
}

func writeFixtureFile(t *testing.T, path, data string, mode fs.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*fixture)
		want   string
	}{
		{
			name: "id does not match directory name",
			mutate: func(f *fixture) {
				f.replace("lab", "id: net-ip-01-link-down", "id: net-ip-99-other")
			},
			want: `id: "net-ip-99-other" does not match directory name "net-ip-01-link-down"`,
		},
		{
			name:   "unsupported version",
			mutate: func(f *fixture) { f.replace("lab", "version: 1", "version: 2") },
			want:   "version: must be 1, got 2",
		},
		{
			name:   "unsupported mode",
			mutate: func(f *fixture) { f.replace("lab", "modes: [guided]", "modes: [guided, real]") },
			want:   `modes: "real" is not supported`,
		},
		{
			name:   "empty modes",
			mutate: func(f *fixture) { f.replace("lab", "modes: [guided]", "modes: []") },
			want:   "modes: must not be empty",
		},
		{
			name:   "unsupported environment",
			mutate: func(f *fixture) { f.replace("lab", "environment: container", "environment: vm") },
			want:   `environment: must be "container", got "vm"`,
		},
		{
			name:   "level out of range",
			mutate: func(f *fixture) { f.replace("lab", "level: 2", "level: 6") },
			want:   "level: must be between 1 and 5, got 6",
		},
		{
			name: "unsupported param generator",
			mutate: func(f *fixture) {
				f.replace("lab", "iface:  { gen: const, value: eth1 }", "iface:  { gen: choice, value: eth1 }")
			},
			want: `params.iface: gen must be "const", got "choice"`,
		},
		{
			name: "empty param value",
			mutate: func(f *fixture) {
				f.replace("lab", "iface:  { gen: const, value: eth1 }", `iface:  { gen: const, value: "" }`)
			},
			want: "params.iface: value must not be empty",
		},
		{
			name: "param references a later param",
			mutate: func(f *fixture) {
				f.replace("lab", "node_a: { gen: const, value: web01 }", `node_a: { gen: const, value: "{{iface}}" }`)
			},
			want: `params.node_a: unknown param "iface"`,
		},
		{
			name:   "ticket references an unknown param",
			mutate: func(f *fixture) { f.replace("lab", "{{node_a}} cannot reach", "{{node_x}} cannot reach") },
			want:   `ticket.en: unknown param "node_x"`,
		},
		{
			name: "checkpoint title references an unknown param",
			mutate: func(f *fixture) {
				f.replace("lab", `{ zh: "{{iface}} 已 UP", en: "{{iface}} is up" }`, `{ zh: "x", en: "{{nic}} is up" }`)
			},
			want: `checkpoints[0].title.en: unknown param "nic"`,
		},
		{
			name:   "checkpoint node is not in the topology",
			mutate: func(f *fixture) { f.replace("lab", "node: web01", "node: web99") },
			want:   `checkpoints[0].node: node "web99" is not declared in topology.yaml`,
		},
		{
			name:   "duplicate checkpoint id",
			mutate: func(f *fixture) { f.replace("lab", "id: ping-peer", "id: link-up") },
			want:   `checkpoints[1].id: duplicate checkpoint id "link-up"`,
		},
		{
			name:   "missing checkpoint script",
			mutate: func(f *fixture) { delete(f.files, "checks/02-ping-peer.sh") },
			want:   "checkpoints[1].script:",
		},
		{
			name: "checkpoint script is not executable",
			mutate: func(f *fixture) {
				f.files["checks/01-link-up.sh"] = fixtureFile{"#!/usr/bin/env bash\n", 0o644}
			},
			want: "checkpoints[0].script: checks/01-link-up.sh is not executable",
		},
		{
			name:   "missing setup script",
			mutate: func(f *fixture) { delete(f.files, "setup.sh") },
			want:   "setup:",
		},
		{
			name:   "setup script is not executable",
			mutate: func(f *fixture) { f.files["setup.sh"] = fixtureFile{"#!/usr/bin/env bash\n", 0o644} },
			want:   "setup: setup.sh is not executable",
		},
		{
			name:   "setup script is a directory",
			mutate: func(f *fixture) { f.replace("lab", "setup: setup.sh", "setup: checks") },
			want:   "setup: checks is not a regular file",
		},
		{
			name:   "unsupported node role",
			mutate: func(f *fixture) { f.replace("topology", "db01:  { role: ubuntu }", "db01:  { role: debian }") },
			want:   `nodes.db01.role: "debian" is not supported`,
		},
		{
			name:   "link endpoint node is not declared",
			mutate: func(f *fixture) { f.replace("topology", `"db01:eth1"]`, `"db99:eth1"]`) },
			want:   `links[0].endpoints[1]: node "db99" is not declared in topology.yaml`,
		},
		{
			name:   "link endpoint uses eth0",
			mutate: func(f *fixture) { f.replace("topology", `"web01:eth1"`, `"web01:eth0"`) },
			want:   `links[0].endpoints[0]: interface "eth0" is reserved`,
		},
		{
			name:   "node appears twice on the same link",
			mutate: func(f *fixture) { f.replace("topology", `"db01:eth1"]`, `"web01:eth2"]`) },
			want:   `links[0].endpoints: node "web01" appears twice on the same link`,
		},
		{
			name:   "address for a node that is not an endpoint",
			mutate: func(f *fixture) { f.replace("topology", `db01: "{{ip_b}}/24"`, `db99: "{{ip_b}}/24"`) },
			want:   `links[0].addresses: node "db99" is not an endpoint of this link`,
		},
		{
			name:   "missing address for an endpoint",
			mutate: func(f *fixture) { f.replace("topology", `, db01: "{{ip_b}}/24"`, "") },
			want:   `links[0].addresses: node "db01" has no address`,
		},
		{
			name:   "address is not a valid CIDR",
			mutate: func(f *fixture) { f.replace("topology", `db01: "{{ip_b}}/24"`, `db01: "{{ip_b}}"`) },
			want:   "links[0].addresses.db01:",
		},
		{
			name: "address is outside the link subnet",
			mutate: func(f *fixture) {
				f.replace("lab", "ip_b:   { gen: const, value: 10.0.5.20 }", "ip_b:   { gen: const, value: 10.0.9.20 }")
			},
			want: "links[0].addresses.db01: 10.0.9.20/24 is outside subnet 10.0.5.0/24",
		},
		{
			name:   "subnet is not a valid CIDR",
			mutate: func(f *fixture) { f.replace("topology", `subnet: "{{subnet}}"`, `subnet: "10.0.5.0"`) },
			want:   "links[0].subnet:",
		},
		{
			name:   "subnet references an unknown param",
			mutate: func(f *fixture) { f.replace("topology", `subnet: "{{subnet}}"`, `subnet: "{{net}}"`) },
			want:   `links[0].subnet: unknown param "net"`,
		},
		{
			name:   "missing solution file",
			mutate: func(f *fixture) { delete(f.files, "solution.zh.md") },
			want:   "solution.zh:",
		},
		{
			name:   "unknown lab field",
			mutate: func(f *fixture) { f.replace("lab", "topic: net/ip", "topic: net/ip\ntags: [ip]") },
			want:   "tags",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			tt.mutate(f)
			_, err := Load(f.write(t))
			if err == nil {
				t.Fatalf("Load() succeeded, want error containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestValidateReportsEveryError(t *testing.T) {
	f := newFixture()
	f.replace("lab", "version: 1", "version: 3")
	f.replace("lab", "level: 2", "level: 9")
	f.replace("lab", "environment: container", "environment: vm")
	_, err := Load(f.write(t))
	if err == nil {
		t.Fatal("Load() succeeded, want errors")
	}
	for _, want := range []string{"version: must be 1", "level: must be between 1 and 5", `environment: must be "container"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Load() error = %v, want it to contain %q", err, want)
		}
	}
}

func TestValidateAcceptsFixture(t *testing.T) {
	labs, err := Load(newFixture().write(t))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(labs) != 1 {
		t.Fatalf("Load() returned %d labs, want 1", len(labs))
	}
}
