package attempt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AmerDwight/network-skill-lab/internal/store"
)

const tutorialLabYAML = `id: net-tutorial-01
version: 1
title: { zh: "教學", en: "Tutorial" }
topic: net/ip
level: 1
modes: [tutorial, guided]
environment: container
estimated_minutes: 5
ticket: { zh: "{{iface}}", en: "{{iface}}" }
params:
  iface: { gen: const, value: eth1 }
setup: setup.sh
checkpoints:
  - id: link-up
    title: { zh: "UP", en: "up" }
    node: web01
    script: checks/01-link-up.sh
  - id: ping-peer
    title: { zh: "ping", en: "ping" }
    node: web01
    script: checks/01-link-up.sh
    requires: [link-up]
tutorial:
  - checkpoint: link-up
    instruction: { zh: "把 {{iface}} 拉起來", en: "bring {{iface}} up" }
  - checkpoint: ping-peer
    instruction: { zh: "然後 ping", en: "then ping" }
solution: { zh: solution.zh.md, en: solution.en.md }
`

func writeTutorialLab(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	dir := filepath.Join(root, "labs", "net-tutorial-01")
	if err := os.MkdirAll(filepath.Join(dir, "checks"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]struct {
		data string
		mode os.FileMode
	}{
		"lab.yaml":             {tutorialLabYAML, 0o644},
		"topology.yaml":        {precheckTopologyYAML, 0o644},
		"setup.sh":             {"#!/usr/bin/env bash\n", 0o755},
		"checks/01-link-up.sh": {"#!/usr/bin/env bash\n", 0o755},
		"solution.zh.md":       {"zh\n", 0o644},
		"solution.en.md":       {"en\n", 0o644},
	}
	for name, file := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.WriteFile(path, []byte(file.data), file.mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, file.mode); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestViewCarriesTutorialStepsInTutorialMode(t *testing.T) {
	fr := &fakeRunner{steps: provisioningSteps}
	h := newHarnessWithContent(t, fr, writeTutorialLab(t))

	view, err := h.Start(t.Context(), h.user, h.lab.Id, "tutorial")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	h.waitForStatus(t, store.StatusRunning)

	if len(view.TutorialSteps) != 2 {
		t.Fatalf("tutorial steps = %+v, want two", view.TutorialSteps)
	}
	if view.TutorialSteps[0].Checkpoint != "link-up" || view.TutorialSteps[1].Checkpoint != "ping-peer" {
		t.Errorf("step order = %q, %q", view.TutorialSteps[0].Checkpoint, view.TutorialSteps[1].Checkpoint)
	}
	if got := view.TutorialSteps[0].Instruction.Get("en"); got != "bring eth1 up" {
		t.Errorf("instruction = %q, want the params rendered", got)
	}
}

func TestViewHasNoTutorialStepsInGuidedMode(t *testing.T) {
	fr := &fakeRunner{steps: provisioningSteps}
	h := newHarnessWithContent(t, fr, writeTutorialLab(t))

	view, err := h.Start(t.Context(), h.user, h.lab.Id, "guided")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	h.waitForStatus(t, store.StatusRunning)

	if len(view.TutorialSteps) != 0 {
		t.Errorf("tutorial steps = %+v, want none", view.TutorialSteps)
	}
}
