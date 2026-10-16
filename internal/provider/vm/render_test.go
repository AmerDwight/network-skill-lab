package vm

import (
	"encoding/xml"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/goccy/go-yaml"
)

func testSpec() runner.SandboxSpec {
	return runner.SandboxSpec{
		AttemptID: "01JATT",
		Nodes:     []runner.NodeSpec{{Name: "db01", Role: "ubuntu"}, {Name: "r1", Role: "ubuntu"}, {Name: "web01", Role: "ubuntu-nm"}},
		Links: []runner.LinkSpec{
			{Name: "l1", Subnet: "10.0.5.0/24", Endpoints: []runner.EndpointSpec{
				{Node: "web01", Iface: "eth1", Address: "10.0.5.10/24"},
				{Node: "r1", Iface: "eth1", Address: "10.0.5.1/24"},
			}},
			{Name: "l2", Subnet: "10.0.6.0/24", Endpoints: []runner.EndpointSpec{
				{Node: "r1", Iface: "eth2", Address: "10.0.6.1/24"},
				{Node: "db01", Iface: "eth1", Address: "10.0.6.20/24"},
			}},
		},
	}
}

type parsedDomain struct {
	Type   string `xml:"type,attr"`
	Name   string `xml:"name"`
	Memory struct {
		Unit  string `xml:"unit,attr"`
		Value int    `xml:",chardata"`
	} `xml:"memory"`
	VCPU    int `xml:"vcpu"`
	Devices struct {
		Disks []struct {
			Device string `xml:"device,attr"`
			Source struct {
				File string `xml:"file,attr"`
			} `xml:"source"`
			Target struct {
				Bus string `xml:"bus,attr"`
			} `xml:"target"`
		} `xml:"disk"`
		Interfaces []struct {
			Source struct {
				Network string `xml:"network,attr"`
			} `xml:"source"`
		} `xml:"interface"`
		Channels []struct {
			Target struct {
				Type string `xml:"type,attr"`
				Name string `xml:"name,attr"`
			} `xml:"target"`
		} `xml:"channel"`
		Serial struct {
			Source struct {
				Path string `xml:"path,attr"`
			} `xml:"source"`
		} `xml:"serial"`
		RNG      *struct{} `xml:"rng"`
		Graphics *struct{} `xml:"graphics"`
	} `xml:"devices"`
}

func TestDomainXML(t *testing.T) {
	spec := testSpec()
	networks := nodeNetworks(spec, "inst1", "default", "r1")
	wantNetworks := []string{"default", networkName("inst1", "01JATT", "l1"), networkName("inst1", "01JATT", "l2")}
	if !slices.Equal(networks, wantNetworks) {
		t.Fatalf("nodeNetworks(r1) = %v, want %v", networks, wantNetworks)
	}

	data, err := domainXML(domainParams{
		Name:      "nsl-inst1-01JATT-r1",
		Owner:     owner{Instance: "inst1", Attempt: "01JATT", Node: "r1"},
		MemoryMiB: 768,
		VCPUs:     2,
		Overlay:   "/var/tmp/nsl-vm/inst1/01JATT/r1.qcow2",
		Seed:      "/var/tmp/nsl-vm/inst1/01JATT/r1-seed.iso",
		Networks:  networks,
		SerialLog: "/var/tmp/nsl-vm/inst1/01JATT/r1-serial.log",
	})
	if err != nil {
		t.Fatal(err)
	}
	var dom parsedDomain
	if err := xml.Unmarshal(data, &dom); err != nil {
		t.Fatalf("domain xml does not parse: %v\n%s", err, data)
	}
	if dom.Type != "kvm" || dom.Name != "nsl-inst1-01JATT-r1" || dom.Memory.Value != 768 || dom.Memory.Unit != "MiB" || dom.VCPU != 2 {
		t.Errorf("domain header = %+v", dom)
	}
	var got []string
	for _, iface := range dom.Devices.Interfaces {
		got = append(got, iface.Source.Network)
	}
	if !slices.Equal(got, wantNetworks) {
		t.Errorf("interfaces = %v, want mgmt first then links in link order %v", got, wantNetworks)
	}
	if len(dom.Devices.Channels) != 1 || dom.Devices.Channels[0].Target.Name != "org.qemu.guest_agent.0" || dom.Devices.Channels[0].Target.Type != "virtio" {
		t.Errorf("channels = %+v, want one guest agent channel", dom.Devices.Channels)
	}
	if len(dom.Devices.Disks) != 2 || dom.Devices.Disks[0].Target.Bus != "virtio" || dom.Devices.Disks[1].Device != "cdrom" || dom.Devices.Disks[1].Target.Bus != "sata" {
		t.Errorf("disks = %+v", dom.Devices.Disks)
	}
	if dom.Devices.Disks[0].Source.File != "/var/tmp/nsl-vm/inst1/01JATT/r1.qcow2" {
		t.Errorf("disk source = %q", dom.Devices.Disks[0].Source.File)
	}
	if dom.Devices.Serial.Source.Path != "/var/tmp/nsl-vm/inst1/01JATT/r1-serial.log" {
		t.Errorf("serial path = %q", dom.Devices.Serial.Source.Path)
	}
	if dom.Devices.RNG == nil || dom.Devices.Graphics != nil {
		t.Errorf("want an rng device and no graphics: %s", data)
	}
	o, ok := parseOwner(data)
	if !ok || o != (owner{Instance: "inst1", Attempt: "01JATT", Node: "r1"}) {
		t.Errorf("parseOwner = %+v, %v", o, ok)
	}
}

func TestDomainXMLEscapes(t *testing.T) {
	data, err := domainXML(domainParams{Name: "a'<b>", Owner: owner{Instance: `x"&y`, Attempt: "a"}, Networks: []string{"default"}})
	if err != nil {
		t.Fatal(err)
	}
	var dom parsedDomain
	if err := xml.Unmarshal(data, &dom); err != nil {
		t.Fatalf("escaped xml does not parse: %v\n%s", err, data)
	}
	if o, ok := parseOwner(data); !ok || o.Instance != `x"&y` || dom.Name != "a'<b>" {
		t.Errorf("round trip = %q %+v", dom.Name, o)
	}
}

func TestNetworkXML(t *testing.T) {
	data, err := networkXML(networkParams{Name: "nsl-inst1-01JATT-l1", Owner: owner{Instance: "inst1", Attempt: "01JATT", Link: "l1"}})
	if err != nil {
		t.Fatal(err)
	}
	var net struct {
		Name   string    `xml:"name"`
		IP     *struct{} `xml:"ip"`
		Fwd    *struct{} `xml:"forward"`
		Bridge struct {
			STP  string `xml:"stp,attr"`
			Name string `xml:"name,attr"`
		} `xml:"bridge"`
	}
	if err := xml.Unmarshal(data, &net); err != nil {
		t.Fatalf("network xml does not parse: %v\n%s", err, data)
	}
	if net.Name != "nsl-inst1-01JATT-l1" || net.IP != nil || net.Fwd != nil || net.Bridge.STP != "off" || net.Bridge.Name != "" {
		t.Errorf("network = %+v\n%s", net, data)
	}
	if o, ok := parseOwner(data); !ok || o != (owner{Instance: "inst1", Attempt: "01JATT", Link: "l1"}) {
		t.Errorf("parseOwner = %+v, %v", o, ok)
	}
}

func TestParseOwnerIgnoresForeignXML(t *testing.T) {
	for _, data := range []string{
		"<domain><name>win10</name></domain>",
		"<domain><metadata><other:x xmlns:other='urn:other' instance='inst1'/></metadata></domain>",
		"not xml",
	} {
		if o, ok := parseOwner([]byte(data)); ok {
			t.Errorf("parseOwner(%q) = %+v, want no owner", data, o)
		}
	}
}

func TestObjectName(t *testing.T) {
	if got, want := domainName("vm-01", "01JATT", "web01"), "nsl-vm-01-01JATT-web01"; got != want {
		t.Errorf("domainName = %q, want %q", got, want)
	}
	if got, want := networkName("a b", "x/y", "l1"), "nsl-a_b-x_y-l1"; got != want {
		t.Errorf("networkName = %q, want %q", got, want)
	}
	long := domainName(strings.Repeat("i", 40), "01JAZ3Q4V0M1W2X3Y4Z5ABCDEF", "web01")
	if len(long) != maxNameLen || !strings.HasPrefix(long, namePrefix) {
		t.Errorf("long name = %q (%d chars), want %d chars with the nsl- prefix", long, len(long), maxNameLen)
	}
	if other := domainName(strings.Repeat("i", 40), "01JAZ3Q4V0M1W2X3Y4Z5ABCDEF", "web02"); other == long {
		t.Errorf("truncated names collide: %q", long)
	}
	if got := sandboxDir("/w", "..", "a/b"); got != "/w/_../a_b" {
		t.Errorf("sandboxDir = %q", got)
	}
}

func testSeed(t *testing.T, env map[string]string) seed {
	t.Helper()
	host, err := newKeyPair("host")
	if err != nil {
		t.Fatal(err)
	}
	user, err := newKeyPair("user")
	if err != nil {
		t.Fatal(err)
	}
	return seed{instanceID: "nsl-inst1-01JATT-web01-abcd", hostname: "web01", userKey: user.public, hostKey: host, env: env}
}

func TestUserDataCarriesTheBootstrapEnv(t *testing.T) {
	spec := testSpec()
	node := spec.Nodes[2]
	want := spec.BootstrapEnv(node, "tok")
	s := testSeed(t, want)

	data, err := s.userData()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "#cloud-config\n") {
		t.Fatalf("user-data must start with #cloud-config:\n%s", data)
	}
	var cfg cloudConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("user-data is not yaml: %v\n%s", err, data)
	}
	if cfg.Hostname != "web01" || !cfg.ManageEtcHosts || cfg.SSHPwauth {
		t.Errorf("user-data header = %+v", cfg)
	}
	if len(cfg.Users) != 1 || cfg.Users[0].Name != "nsl" || !slices.Equal(cfg.Users[0].SSHAuthorizedKeys, []string{s.userKey}) {
		t.Errorf("users = %+v", cfg.Users)
	}
	if cfg.SSHKeys["ed25519_public"] != s.hostKey.public || cfg.SSHKeys["ed25519_private"] != string(s.hostKey.private) {
		t.Errorf("ssh_keys do not carry the generated host key")
	}
	if len(cfg.WriteFiles) != 1 || cfg.WriteFiles[0].Path != "/etc/nsl/bootstrap.env" {
		t.Fatalf("write_files = %+v", cfg.WriteFiles)
	}
	if got := sourceEnv(t, cfg.WriteFiles[0].Content, slices.Collect(maps.Keys(want))); !maps.Equal(got, want) {
		t.Errorf("bootstrap.env = %v, want the provider bootstrap env %v", got, want)
	}

	if meta := string(s.metaData()); meta != "instance-id: nsl-inst1-01JATT-web01-abcd\nlocal-hostname: web01\n" {
		t.Errorf("meta-data = %q", meta)
	}
}

func sourceEnv(t *testing.T, content string, keys []string) map[string]string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bootstrap.env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	script := `. "$1"; shift; for k in "$@"; do eval "printf '%s=%s\n' \"\$k\" \"\$$k\""; done`
	out, err := exec.Command("sh", append([]string{"-c", script, "sh", path}, keys...)...).Output()
	if err != nil {
		t.Fatalf("source bootstrap.env: %v\n%s", err, content)
	}
	got := map[string]string{}
	for line := range strings.Lines(string(out)) {
		key, value, _ := strings.Cut(strings.TrimSuffix(line, "\n"), "=")
		got[key] = value
	}
	return got
}

func TestEnvFileQuotesValues(t *testing.T) {
	env := map[string]string{"A": "it's $HOME `x`", "B": ""}
	if got := sourceEnv(t, envFile(env), []string{"A", "B"}); !maps.Equal(got, env) {
		t.Errorf("sourced env = %v, want %v", got, env)
	}
}

func TestNetworkConfig(t *testing.T) {
	var cfg struct {
		Version   int                       `yaml:"version"`
		Ethernets map[string]map[string]any `yaml:"ethernets"`
	}
	if err := yaml.Unmarshal(networkConfig(), &cfg); err != nil {
		t.Fatal(err)
	}
	eth0, ok := cfg.Ethernets["eth0"]
	if cfg.Version != 2 || len(cfg.Ethernets) != 1 || !ok || eth0["dhcp4"] != true || eth0["dhcp-identifier"] != "mac" {
		t.Errorf("network-config = %+v", cfg)
	}
}
