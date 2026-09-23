package content

import (
	"strings"
	"testing"
)

const casesBlock = `cases:
  - { file: cases/default.yaml, weight: 3 }
  - { file: cases/wrong-mtu.yaml, weight: 1 }
`

const precheckBlock = `precheck: { script: precheck.sh, retries: 3 }
`

const tutorialBlock = `tutorial:
  - checkpoint: link-up
    instruction: { zh: "先看介面", en: "inspect the interface" }
  - checkpoint: ping-peer
    instruction: { zh: "再 ping", en: "then ping" }
`

func withCases(f *fixture) {
	f.replace("lab", "setup: setup.sh", casesBlock+"setup: setup.sh")
	f.files["cases/default.yaml"] = fixtureFile{"params: { fault: { gen: const, value: link } }\n", 0o644}
	f.files["cases/wrong-mtu.yaml"] = fixtureFile{"params: { fault: { gen: const, value: mtu } }\nsetup_env: { NSL_FAULT_MTU: \"1200\" }\n", 0o644}
}

func withPrecheck(f *fixture) {
	f.replace("lab", "setup: setup.sh", precheckBlock+"setup: setup.sh")
	f.files["precheck.sh"] = fixtureFile{"#!/usr/bin/env bash\n", 0o755}
}

func withTutorial(f *fixture) {
	f.replace("lab", "modes: [guided]", "modes: [tutorial, guided]")
	f.replace("lab", "solution:", tutorialBlock+"solution:")
}

func withRequires(f *fixture) {
	f.replace("lab", "script: checks/02-ping-peer.sh", "script: checks/02-ping-peer.sh\n    requires: [link-up]")
}

func loadFixture(t *testing.T, mutate func(*fixture)) ([]Lab, error) {
	t.Helper()
	f := newFixture()
	if mutate != nil {
		mutate(f)
	}
	return Load(f.write(t))
}

func requireValid(t *testing.T, mutate func(*fixture)) []Lab {
	t.Helper()
	labs, err := loadFixture(t, mutate)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return labs
}

func requireInvalid(t *testing.T, mutate func(*fixture), want string) {
	t.Helper()
	_, err := loadFixture(t, mutate)
	if err == nil {
		t.Fatalf("Load() succeeded, want an error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("Load() error = %v, want it to contain %q", err, want)
	}
}

func TestValidateCases(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*fixture)
		want   string
	}{
		{
			name:   "valid cases",
			mutate: withCases,
		},
		{
			name: "missing case file",
			mutate: func(f *fixture) {
				withCases(f)
				delete(f.files, "cases/wrong-mtu.yaml")
			},
			want: "cases[1].file:",
		},
		{
			name: "weight is zero",
			mutate: func(f *fixture) {
				withCases(f)
				f.replace("lab", "weight: 1 }", "weight: 0 }")
			},
			want: "cases[1].weight: must be a positive integer, got 0",
		},
		{
			name: "weight is negative",
			mutate: func(f *fixture) {
				withCases(f)
				f.replace("lab", "weight: 3 }", "weight: -2 }")
			},
			want: "cases[0].weight: must be a positive integer, got -2",
		},
		{
			name: "duplicate case id",
			mutate: func(f *fixture) {
				withCases(f)
				f.replace("lab", "file: cases/wrong-mtu.yaml", "file: cases/default.yaml")
			},
			want: `cases[1].file: duplicate case id "default"`,
		},
		{
			name: "case param uses an unknown generator",
			mutate: func(f *fixture) {
				withCases(f)
				f.files["cases/wrong-mtu.yaml"] = fixtureFile{"params: { fault: { gen: dice, value: mtu } }\n", 0o644}
			},
			want: `cases[1].params.fault: gen "dice" is not supported`,
		},
		{
			name: "case param references an unknown param",
			mutate: func(f *fixture) {
				withCases(f)
				f.files["cases/wrong-mtu.yaml"] = fixtureFile{"params: { fault: { gen: const, value: \"{{nope}}\" } }\n", 0o644}
			},
			want: `cases[1].params.fault: unknown param "nope"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.want == "" {
				requireValid(t, tt.mutate)
				return
			}
			requireInvalid(t, tt.mutate, tt.want)
		})
	}
}

func TestLoadReadsCases(t *testing.T) {
	lab := requireValid(t, withCases)[0]
	if len(lab.Cases) != 2 {
		t.Fatalf("got %d cases, want 2", len(lab.Cases))
	}
	if lab.Cases[0].ID != "default" || lab.Cases[1].ID != "wrong-mtu" {
		t.Errorf("case ids = %q, %q", lab.Cases[0].ID, lab.Cases[1].ID)
	}
	if lab.Cases[1].SetupEnv["NSL_FAULT_MTU"] != "1200" {
		t.Errorf("setup env = %v", lab.Cases[1].SetupEnv)
	}

	resolved, err := lab.ResolveFor(1)
	if err != nil {
		t.Fatalf("ResolveFor() error = %v", err)
	}
	if resolved.Params["fault"] == "" {
		t.Errorf("the case param was not applied: %v", resolved.Params)
	}
}

func TestValidatePrecheck(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*fixture)
		want   string
	}{
		{
			name:   "valid precheck",
			mutate: withPrecheck,
		},
		{
			name: "missing script",
			mutate: func(f *fixture) {
				withPrecheck(f)
				delete(f.files, "precheck.sh")
			},
			want: "precheck.script:",
		},
		{
			name: "script is not executable",
			mutate: func(f *fixture) {
				withPrecheck(f)
				f.files["precheck.sh"] = fixtureFile{"#!/usr/bin/env bash\n", 0o644}
			},
			want: "precheck.script: precheck.sh is not executable",
		},
		{
			name: "retries above the maximum",
			mutate: func(f *fixture) {
				withPrecheck(f)
				f.replace("lab", "retries: 3", "retries: 6")
			},
			want: "precheck.retries: must be between 1 and 5, got 6",
		},
		{
			name: "retries below the minimum",
			mutate: func(f *fixture) {
				withPrecheck(f)
				f.replace("lab", "retries: 3", "retries: -1")
			},
			want: "precheck.retries: must be between 1 and 5, got -1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.want == "" {
				requireValid(t, tt.mutate)
				return
			}
			requireInvalid(t, tt.mutate, tt.want)
		})
	}
}

func TestPrecheckRetriesDefaultToThree(t *testing.T) {
	lab := requireValid(t, func(f *fixture) {
		withPrecheck(f)
		f.replace("lab", "precheck: { script: precheck.sh, retries: 3 }", "precheck: { script: precheck.sh }")
	})[0]
	if lab.Precheck == nil || lab.Precheck.Retries != 3 {
		t.Errorf("precheck = %+v, want 3 retries", lab.Precheck)
	}
}

func TestValidateRequires(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*fixture)
		want   string
	}{
		{
			name:   "valid requires",
			mutate: withRequires,
		},
		{
			name: "unknown checkpoint",
			mutate: func(f *fixture) {
				f.replace("lab", "script: checks/02-ping-peer.sh", "script: checks/02-ping-peer.sh\n    requires: [nope]")
			},
			want: `checkpoints[1].requires: checkpoint "nope" does not exist`,
		},
		{
			name: "self reference",
			mutate: func(f *fixture) {
				f.replace("lab", "script: checks/02-ping-peer.sh", "script: checks/02-ping-peer.sh\n    requires: [ping-peer]")
			},
			want: "cycle through",
		},
		{
			name: "cycle between two checkpoints",
			mutate: func(f *fixture) {
				withRequires(f)
				f.replace("lab", "script: checks/01-link-up.sh", "script: checks/01-link-up.sh\n    requires: [ping-peer]")
			},
			want: "cycle through",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.want == "" {
				requireValid(t, tt.mutate)
				return
			}
			requireInvalid(t, tt.mutate, tt.want)
		})
	}
}

func TestValidateTutorial(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*fixture)
		want   string
	}{
		{
			name:   "valid tutorial",
			mutate: withTutorial,
		},
		{
			name: "valid tutorial ordered by requires",
			mutate: func(f *fixture) {
				withRequires(f)
				withTutorial(f)
			},
		},
		{
			name:   "tutorial without the tutorial mode",
			mutate: func(f *fixture) { f.replace("lab", "solution:", tutorialBlock+"solution:") },
			want:   `tutorial: is only allowed when modes contains "tutorial"`,
		},
		{
			name:   "tutorial mode without steps",
			mutate: func(f *fixture) { f.replace("lab", "modes: [guided]", "modes: [tutorial, guided]") },
			want:   `tutorial: must not be empty when modes contains "tutorial"`,
		},
		{
			name: "a visible checkpoint has no step",
			mutate: func(f *fixture) {
				withTutorial(f)
				f.replace("lab", "  - checkpoint: ping-peer\n    instruction: { zh: \"再 ping\", en: \"then ping\" }\n", "")
			},
			want: `tutorial: checkpoint "ping-peer" has no step`,
		},
		{
			name: "duplicate step",
			mutate: func(f *fixture) {
				withTutorial(f)
				f.replace("lab", "  - checkpoint: ping-peer", "  - checkpoint: link-up")
			},
			want: `tutorial[1].checkpoint: duplicate step for checkpoint "link-up"`,
		},
		{
			name: "step for an unknown checkpoint",
			mutate: func(f *fixture) {
				withTutorial(f)
				f.replace("lab", "  - checkpoint: ping-peer", "  - checkpoint: nope")
			},
			want: `tutorial[1].checkpoint: "nope" is not a visible checkpoint`,
		},
		{
			name: "steps contradict requires",
			mutate: func(f *fixture) {
				withRequires(f)
				withTutorial(f)
				f.replace("lab", tutorialBlock, `tutorial:
  - checkpoint: ping-peer
    instruction: { zh: "再 ping", en: "then ping" }
  - checkpoint: link-up
    instruction: { zh: "先看介面", en: "inspect the interface" }
`)
			},
			want: `"ping-peer" comes before "link-up", which it requires`,
		},
		{
			name: "hidden checkpoint under the tutorial mode",
			mutate: func(f *fixture) {
				withTutorial(f)
				f.replace("lab", "script: checks/02-ping-peer.sh\n    visible: true", "script: checks/02-ping-peer.sh\n    visible: false")
			},
			want: `checkpoints[1].visible: must be true when modes contains "tutorial"`,
		},
		{
			name: "empty instruction",
			mutate: func(f *fixture) {
				withTutorial(f)
				f.replace("lab", `instruction: { zh: "再 ping", en: "then ping" }`, `instruction: { zh: "再 ping", en: "" }`)
			},
			want: "tutorial[1].instruction.en: must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.want == "" {
				requireValid(t, tt.mutate)
				return
			}
			requireInvalid(t, tt.mutate, tt.want)
		})
	}
}

func TestValidateK3sOptions(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*fixture)
		want   string
	}{
		{
			name: "k3s options on a k3s server",
			mutate: func(f *fixture) {
				f.replace("topology", "web01: { role: ubuntu }", "web01: { role: k3s-server, k3s: { disable: [traefik] } }")
			},
		},
		{
			name: "k3s options on an ubuntu node",
			mutate: func(f *fixture) {
				f.replace("topology", "web01: { role: ubuntu }", "web01: { role: ubuntu, k3s: { disable: [traefik] } }")
			},
			want: `nodes.web01.k3s: is only allowed on role "k3s-server", got "ubuntu"`,
		},
		{
			name: "empty component name",
			mutate: func(f *fixture) {
				f.replace("topology", "web01: { role: ubuntu }", `web01: { role: k3s-server, k3s: { disable: [""] } }`)
			},
			want: "nodes.web01.k3s.disable[0]: must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.want == "" {
				requireValid(t, tt.mutate)
				return
			}
			requireInvalid(t, tt.mutate, tt.want)
		})
	}
}

func TestCheckpointVisibilityDefaultsToTrue(t *testing.T) {
	lab := requireValid(t, func(f *fixture) {
		f.replace("lab", "script: checks/01-link-up.sh\n    visible: true", "script: checks/01-link-up.sh")
	})[0]
	if !lab.Checkpoints[0].Visible {
		t.Error("a checkpoint without a visible field is hidden, want visible")
	}
}

func TestHiddenCheckpointsAreAllowedOutsideTutorialMode(t *testing.T) {
	lab := requireValid(t, func(f *fixture) {
		f.replace("lab", "script: checks/02-ping-peer.sh\n    visible: true", "script: checks/02-ping-peer.sh\n    visible: false")
	})[0]
	if lab.Checkpoints[1].Visible {
		t.Error("the checkpoint is visible, want hidden")
	}
}

func TestInternetDefaultsToTrue(t *testing.T) {
	lab := requireValid(t, nil)[0]
	if !lab.InternetEnabled() {
		t.Error("InternetEnabled() = false, want true by default")
	}

	off := requireValid(t, func(f *fixture) {
		f.replace("lab", "setup: setup.sh", "internet: false\nsetup: setup.sh")
	})[0]
	if off.InternetEnabled() {
		t.Error("InternetEnabled() = true, want false")
	}
}

func TestParamGeneratorFieldsAreChecked(t *testing.T) {
	tests := []struct {
		name  string
		param string
		want  string
	}{
		{"cidr without a base", "subnet: { gen: cidr, prefix: 24 }", `gen "cidr" requires "base"`},
		{"cidr with a foreign field", "subnet: { gen: cidr, base: 10.0.0.0/8, prefix: 24, of: [a] }", `"of" is not a field of gen "cidr"`},
		{"ip_in without a selector", "subnet: { gen: ip_in, subnet: 10.0.5.0/24 }", `gen "ip_in" requires index or range`},
		{"ip_in with both selectors", "subnet: { gen: ip_in, subnet: 10.0.5.0/24, index: 3, range: [1, 5] }", "index and range are mutually exclusive"},
		{"ip_in with a one-sided range", "subnet: { gen: ip_in, subnet: 10.0.5.0/24, range: [1] }", "range must hold exactly two values, got 1"},
		{"int without bounds", "subnet: { gen: int }", `gen "int" requires "min"`},
		{"const without a value", "subnet: { gen: const }", `gen "const" requires "value"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireInvalid(t, func(f *fixture) {
				f.replace("lab", "subnet: { gen: const, value: 10.0.5.0/24 }", tt.param)
			}, tt.want)
		})
	}
}
