package attempt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

const precheckLabYAML = `id: net-precheck-01
version: 1
title: { zh: "預檢", en: "Precheck" }
topic: net/ip
level: 2
modes: [guided]
environment: container
estimated_minutes: 5
ticket: { zh: "{{fault}}", en: "{{fault}}" }
params:
  fault: { gen: choice, of: [link, mtu] }
  offset: { gen: int, min: 1, max: 250 }
cases:
  - { file: cases/default.yaml, weight: 1 }
precheck: { script: precheck.sh, retries: 3 }
setup: setup.sh
checkpoints:
  - id: link-up
    title: { zh: "UP", en: "up" }
    node: web01
    script: checks/01-link-up.sh
  - id: hidden
    title: { zh: "隱藏", en: "hidden" }
    node: db01
    script: checks/01-link-up.sh
    visible: false
solution: { zh: solution.zh.md, en: solution.en.md }
`

const precheckTopologyYAML = `nodes:
  web01: { role: ubuntu }
  db01:  { role: ubuntu }
`

func writePrecheckLab(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	dir := filepath.Join(root, "labs", "net-precheck-01")
	if err := os.MkdirAll(filepath.Join(dir, "checks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "cases"), 0o755); err != nil {
		t.Fatal(err)
	}

	files := map[string]struct {
		data string
		mode os.FileMode
	}{
		"lab.yaml":             {precheckLabYAML, 0o644},
		"topology.yaml":        {precheckTopologyYAML, 0o644},
		"setup.sh":             {"#!/usr/bin/env bash\n", 0o755},
		"precheck.sh":          {"#!/usr/bin/env bash\ntest \"$NSL_FAULT\" = link\n", 0o755},
		"checks/01-link-up.sh": {"#!/usr/bin/env bash\n", 0o755},
		"cases/default.yaml":   {"setup_env: { NSL_FAULT_MTU: \"1200\" }\n", 0o644},
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

func newPrecheckHarness(t *testing.T, fr *fakeRunner) *harness {
	t.Helper()
	return newHarnessWithContent(t, fr, writePrecheckLab(t))
}

func pass() (runner.ExecResult, error) { return runner.ExecResult{ExitCode: 0}, nil }
func fail(stderr string) (runner.ExecResult, error) {
	return runner.ExecResult{ExitCode: 1, Stderr: []byte(stderr)}, nil
}

func (h *harness) startAndWait(t *testing.T, status string) (string, Event) {
	t.Helper()
	view, err := h.Start(t.Context(), h.user, h.lab.Id, "guided")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	return view.Id, h.waitForStatus(t, status)
}

func TestPrecheckPassesOnTheFirstAttempt(t *testing.T) {
	fr := &fakeRunner{steps: provisioningSteps}
	fr.exec = func(execCall, int) (runner.ExecResult, error) { return pass() }
	h := newPrecheckHarness(t, fr)

	id, _ := h.startAndWait(t, store.StatusRunning)

	if got := fr.provisionCount(); got != 1 {
		t.Errorf("provisioned %d times, want 1", got)
	}
	calls := fr.execCalls()
	if len(calls) != 2 {
		t.Fatalf("ran the precheck %d times, want once per node", len(calls))
	}
	if calls[0].node != "db01" || calls[1].node != "web01" {
		t.Errorf("precheck node order = %q, %q, want topology order", calls[0].node, calls[1].node)
	}
	for _, call := range calls {
		if strings.Join(call.cmd, " ") != "bash -s" {
			t.Errorf("command = %v, want bash -s", call.cmd)
		}
		if !strings.Contains(call.script, "NSL_FAULT") {
			t.Errorf("script = %q, want the precheck script", call.script)
		}
		if call.env["NSL_NODE"] != call.node {
			t.Errorf("NSL_NODE = %q, want %q", call.env["NSL_NODE"], call.node)
		}
		if call.env["NSL_FAULT"] == "" {
			t.Errorf("env is missing the params: %v", call.env)
		}
		if call.env["NSL_FAULT_MTU"] != "1200" {
			t.Errorf("env is missing setup_env: %v", call.env)
		}
	}

	att := h.attempt(t, id)
	if att.CaseID != "default" {
		t.Errorf("case id = %q", att.CaseID)
	}
	if att.Seed == nil {
		t.Error("the seed was not stored")
	}
	if len(fr.destroys()) != 0 {
		t.Errorf("destroyed %v, want nothing", fr.destroys())
	}
}

func TestPrecheckRetriesWithANewSeed(t *testing.T) {
	fr := &fakeRunner{steps: provisioningSteps}
	fr.exec = func(_ execCall, nth int) (runner.ExecResult, error) {
		if nth == 1 {
			return fail("the first sandbox is wrong")
		}
		return pass()
	}
	h := newPrecheckHarness(t, fr)

	view, err := h.Start(t.Context(), h.user, h.lab.Id, "guided")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if view.Seed == nil {
		t.Fatal("the first view carries no seed")
	}
	h.waitForStatus(t, store.StatusRunning)

	if got := fr.provisionCount(); got != 2 {
		t.Errorf("provisioned %d times, want 2", got)
	}
	if got := fr.destroys(); len(got) != 1 || got[0] != view.Id {
		t.Errorf("destroyed %v, want the first sandbox once", got)
	}

	att := h.attempt(t, view.Id)
	if att.Seed == nil {
		t.Fatal("the seed was not stored")
	}
	if *att.Seed != *view.Seed+1 {
		t.Errorf("seed = %d, want %d", *att.Seed, *view.Seed+1)
	}
	if att.CaseID != "default" {
		t.Errorf("case id = %q", att.CaseID)
	}
	if att.Status != store.StatusRunning {
		t.Errorf("status = %q", att.Status)
	}

	retried, err := h.Get(t.Context(), view.Id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if retried.Params["fault"] != fr.execCalls()[1].env["NSL_FAULT"] {
		t.Errorf("the stored params %v do not match the precheck env %v", retried.Params, fr.execCalls()[1].env)
	}
}

func TestPrecheckRetriesPublishTheAttemptNumber(t *testing.T) {
	fr := &fakeRunner{steps: provisioningSteps}
	fr.exec = func(_ execCall, nth int) (runner.ExecResult, error) {
		if nth <= 2 {
			return fail("still broken")
		}
		return pass()
	}
	h := newPrecheckHarness(t, fr)

	if _, err := h.Start(t.Context(), h.user, h.lab.Id, "guided"); err != nil {
		t.Fatalf("start: %v", err)
	}

	var numbers []int
	for {
		ev := h.next(t)
		if ev.Type == EventProvisioning && ev.Step == StepPrecheck {
			numbers = append(numbers, ev.Attempt)
		}
		if ev.Type == EventStatus && ev.Status == store.StatusRunning {
			break
		}
	}
	if len(numbers) != 2 || numbers[0] != 2 || numbers[1] != 3 {
		t.Errorf("precheck attempt numbers = %v, want [2 3]", numbers)
	}
}

func TestPrecheckGivesUpAfterTheLastAttempt(t *testing.T) {
	fr := &fakeRunner{steps: provisioningSteps}
	fr.exec = func(execCall, int) (runner.ExecResult, error) {
		return fail(strings.Repeat("noise\n", 30) + "the real reason")
	}
	h := newPrecheckHarness(t, fr)

	id, ev := h.startAndWait(t, store.StatusError)

	if got := fr.provisionCount(); got != 3 {
		t.Errorf("provisioned %d times, want 3", got)
	}
	if got := len(fr.destroys()); got < 3 {
		t.Errorf("destroyed %d sandboxes, want one per attempt", got)
	}
	if !strings.Contains(ev.ErrorMessage, "precheck failed after 3 attempts on db01") {
		t.Errorf("error message = %q", ev.ErrorMessage)
	}
	if !strings.Contains(ev.ErrorMessage, "the real reason") {
		t.Errorf("error message does not carry the stderr tail: %q", ev.ErrorMessage)
	}
	if strings.Count(ev.ErrorMessage, "noise") != 19 {
		t.Errorf("error message does not carry the last 20 stderr lines: %q", ev.ErrorMessage)
	}

	att := h.attempt(t, id)
	if att.Status != store.StatusError {
		t.Errorf("stored status = %q", att.Status)
	}
}

func TestPrecheckTimeoutCountsAsAFailure(t *testing.T) {
	fr := &fakeRunner{steps: provisioningSteps}
	fr.exec = func(execCall, int) (runner.ExecResult, error) {
		return runner.ExecResult{TimedOut: true}, nil
	}
	h := newPrecheckHarness(t, fr)

	_, ev := h.startAndWait(t, store.StatusError)
	if !strings.Contains(ev.ErrorMessage, "timed out") {
		t.Errorf("error message = %q", ev.ErrorMessage)
	}
}

func TestViewExposesHiddenCheckpointsAndEnv(t *testing.T) {
	fr := &fakeRunner{steps: provisioningSteps}
	fr.exec = func(execCall, int) (runner.ExecResult, error) { return pass() }
	h := newPrecheckHarness(t, fr)

	id, _ := h.startAndWait(t, store.StatusRunning)
	view, err := h.Get(t.Context(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if len(view.Checkpoints) != 2 {
		t.Fatalf("checkpoints = %+v, want both the visible and the hidden one", view.Checkpoints)
	}
	if !view.Checkpoints[0].Visible || view.Checkpoints[1].Visible {
		t.Errorf("visibility = %v, %v, want true, false", view.Checkpoints[0].Visible, view.Checkpoints[1].Visible)
	}
	if view.CaseID != "default" {
		t.Errorf("case id = %q", view.CaseID)
	}
	if view.Seed == nil {
		t.Error("the view carries no seed")
	}
	if view.Env["NSL_FAULT_MTU"] != "1200" || view.Env["NSL_FAULT"] == "" {
		t.Errorf("env = %v", view.Env)
	}
	if len(view.TutorialSteps) != 0 {
		t.Errorf("tutorial steps = %v, want none outside the tutorial mode", view.TutorialSteps)
	}
}

func TestLabWithoutPrecheckNeverExecs(t *testing.T) {
	fr := &fakeRunner{steps: provisioningSteps}
	h := newHarness(t, fr)

	h.startRunning(t)

	if got := fr.execCalls(); len(got) != 0 {
		t.Errorf("ran %d commands, want none", len(got))
	}
	if got := fr.provisionCount(); got != 1 {
		t.Errorf("provisioned %d times, want 1", got)
	}
}
