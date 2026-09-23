package lint

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const labDir = "labs/net-ip-01-link-down/"

const labYAML = `id: net-ip-01-link-down
version: 1
title: { zh: "伺服器連不到對外", en: "Server lost connectivity" }
topic: net/nope
level: 2
modes: [guided]
environment: container
estimated_minutes: 10
ticket:
  zh: "{{node_a}} 無法連到 {{node_b}}。"
  en: "{{node_a}} cannot reach {{node_b}}."
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
solution: { zh: solution.zh.md, en: solution.en.md }
`

const topologyYAML = `nodes:
  web01: { role: ubuntu }
  db01:  { role: ubuntu }
links:
  - endpoints: ["web01:eth1", "db01:eth1"]
    subnet: "{{subnet}}"
    addresses: { web01: "{{ip_a}}/24", db01: "{{ip_b}}/24" }
`

const topicsYAML = `- id: net
  title: { zh: "網路", en: "Networking" }
  children:
    - id: ip
      title: { zh: "介面與路由", en: "Interfaces & routing" }
`

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, data := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		mode := fs.FileMode(0o644)
		if strings.HasSuffix(name, ".sh") {
			mode = 0o755
		}
		if err := os.WriteFile(path, []byte(data), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func brokenTree(t *testing.T) string {
	t.Helper()
	return writeTree(t, map[string]string{
		"topics.yaml":                   topicsYAML,
		labDir + "lab.yaml":             labYAML,
		labDir + "topology.yaml":        topologyYAML,
		labDir + "setup.sh":             "#!/usr/bin/env bash\nif true\n",
		labDir + "precheck.sh":          "#!/usr/bin/env bash\nping -c 1 10.0.5.20\n",
		labDir + "checks/01-link-up.sh": "#!/usr/bin/env bash\nip link set eth1 up\n",
		labDir + "solution.zh.md":       "zh\n",
		labDir + "solution.en.md":       "en\n",
	})
}

func TestRunReportsErrorsAndWarnings(t *testing.T) {
	root := brokenTree(t)

	findings, err := Run(root)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	lines := make([]string, 0, len(findings))
	for _, finding := range findings {
		lines = append(lines, finding.Line())
	}

	want := []string{
		filepath.Join(root, labDir[:len(labDir)-1]) + `: topic: "net/nope" is not in topics.yaml`,
		"warning: " + filepath.Join(root, labDir, "checks/01-link-up.sh") + `: checks-side-effect: line 2 uses "ip link set", a check script must not change the node`,
		"warning: " + filepath.Join(root, labDir, "precheck.sh") + ": hardcoded-ip: line 2 hardcodes 10.0.5.20, use a param instead",
	}
	for _, line := range want {
		if !slices.Contains(lines, line) {
			t.Errorf("Run() lines = %v, want it to contain %q", lines, line)
		}
	}

	syntax := findingsFor(findings, "bash-syntax")
	if len(syntax) != 1 {
		t.Fatalf("bash-syntax findings = %v, want one", syntax)
	}
	if syntax[0].Path != filepath.Join(root, labDir, "setup.sh") || syntax[0].Severity != SeverityError {
		t.Errorf("bash-syntax finding = %+v", syntax[0])
	}

	for _, rule := range []string{"checks-side-effect", "hardcoded-ip"} {
		for _, finding := range findingsFor(findings, rule) {
			if finding.Severity != SeverityWarning {
				t.Errorf("%s severity = %q, want %q", rule, finding.Severity, SeverityWarning)
			}
		}
	}
	if ExitCode(findings) != 1 {
		t.Errorf("ExitCode() = %d, want 1", ExitCode(findings))
	}
}

func TestRunOnValidContent(t *testing.T) {
	root := writeTree(t, map[string]string{
		"topics.yaml":                   topicsYAML,
		"docs/net/ip/guide.zh.md":       "# 指南\n",
		"docs/net/ip/guide.en.md":       "# Guide\n",
		labDir + "lab.yaml":             strings.Replace(labYAML, "topic: net/nope", "topic: net/ip", 1),
		labDir + "topology.yaml":        topologyYAML,
		labDir + "setup.sh":             "#!/usr/bin/env bash\nip link set \"$NSL_IFACE\" down\n",
		labDir + "checks/01-link-up.sh": "#!/usr/bin/env bash\nip -br link show \"$NSL_IFACE\" | grep -qw UP\n",
		labDir + "solution.zh.md":       "zh\n",
		labDir + "solution.en.md":       "en\n",
	})

	findings, err := Run(root)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("Run() = %v, want no findings", findings)
	}
	if ExitCode(findings) != 0 {
		t.Errorf("ExitCode() = %d, want 0", ExitCode(findings))
	}
}

func TestRunOnRealContent(t *testing.T) {
	findings, err := Run(filepath.Join("..", "..", "content"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if code := ExitCode(findings); code != 0 {
		t.Errorf("ExitCode() = %d, want 0; findings = %v", code, findings)
	}
}

func TestFindingsAreSortedAndSerialized(t *testing.T) {
	findings, err := Run(brokenTree(t))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !slices.IsSortedFunc(findings, func(a, b Finding) int { return strings.Compare(a.Path, b.Path) }) {
		t.Errorf("Run() findings are not sorted by path: %v", findings)
	}

	b, err := json.Marshal([]Finding{{Path: "content/labs/x", Rule: "topic", Message: "unknown", Severity: SeverityError}})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	want := `[{"path":"content/labs/x","rule":"topic","message":"unknown","severity":"error"}]`
	if string(b) != want {
		t.Errorf("Marshal() = %s, want %s", b, want)
	}
}

func TestExitCode(t *testing.T) {
	tests := []struct {
		name     string
		findings []Finding
		want     int
	}{
		{name: "no findings", want: 0},
		{name: "only warnings", findings: []Finding{{Severity: SeverityWarning}}, want: 0},
		{name: "one error", findings: []Finding{{Severity: SeverityWarning}, {Severity: SeverityError}}, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(tt.findings); got != tt.want {
				t.Errorf("ExitCode() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestFindingLine(t *testing.T) {
	err := Finding{Path: "content/topics.yaml", Rule: "topics", Message: "boom", Severity: SeverityError}
	if got, want := err.Line(), "content/topics.yaml: topics: boom"; got != want {
		t.Errorf("Line() = %q, want %q", got, want)
	}
	warning := Finding{Path: "content/a.sh", Rule: "hardcoded-ip", Message: "boom", Severity: SeverityWarning}
	if got, want := warning.Line(), "warning: content/a.sh: hardcoded-ip: boom"; got != want {
		t.Errorf("Line() = %q, want %q", got, want)
	}
}

func findingsFor(findings []Finding, rule string) []Finding {
	var out []Finding
	for _, finding := range findings {
		if finding.Rule == rule {
			out = append(out, finding)
		}
	}
	return out
}
