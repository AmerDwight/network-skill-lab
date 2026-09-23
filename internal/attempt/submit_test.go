package attempt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/AmerDwight/network-skill-lab/internal/content"
	"github.com/AmerDwight/network-skill-lab/internal/content/contenttest"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

const realLabYAML = `id: net-real-01
version: 1
title: { zh: "提交", en: "Submit" }
topic: net/ip
level: 2
modes: [guided, real]
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
  - id: deep-check
    title: { zh: "隱藏", en: "hidden" }
    node: db01
    script: checks/01-link-up.sh
    visible: false
solution: { zh: solution.zh.md, en: solution.en.md }
`

func writeRealLab(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	dir := filepath.Join(root, "labs", "net-real-01")
	if err := os.MkdirAll(filepath.Join(dir, "checks"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]struct {
		data string
		mode os.FileMode
	}{
		"lab.yaml":             {realLabYAML, 0o644},
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

type fakeSweeper struct {
	mu       sync.Mutex
	statuses map[string]string
	err      error
	calls    int
}

func (s *fakeSweeper) SweepNow(_ context.Context, _ string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	out := make(map[string]string, len(s.statuses))
	for id, status := range s.statuses {
		out[id] = status
	}
	return out, nil
}

func (s *fakeSweeper) set(statuses map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statuses = statuses
}

func (s *fakeSweeper) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func newRealHarness(t *testing.T, fr *fakeRunner) (*harness, *fakeSweeper) {
	t.Helper()
	h := newHarnessWithContent(t, fr, writeRealLab(t))
	sweeper := &fakeSweeper{statuses: map[string]string{
		"link-up":    store.CheckpointFail,
		"deep-check": store.CheckpointFail,
	}}
	h.SetSweeper(sweeper)
	return h, sweeper
}

func (h *harness) waitForType(t *testing.T, kind string) Event {
	t.Helper()
	for {
		ev := h.next(t)
		if ev.Type == kind {
			return ev
		}
	}
}

func (h *harness) startRunningIn(t *testing.T, mode string) string {
	t.Helper()
	view, err := h.Start(t.Context(), h.user, h.lab.Id, mode)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	h.waitForStatus(t, store.StatusRunning)
	return view.Id
}

func TestSubmitRejectsOtherModes(t *testing.T) {
	fr := &fakeRunner{steps: provisioningSteps}
	h, sweeper := newRealHarness(t, fr)
	id := h.startRunningIn(t, "guided")

	if _, err := h.Submit(t.Context(), id); !errors.Is(err, ErrModeNotReal) {
		t.Fatalf("Submit in guided mode = %v, want ErrModeNotReal", err)
	}
	if sweeper.count() != 0 {
		t.Errorf("sweeps = %d, want none", sweeper.count())
	}
	if att := h.attempt(t, id); att.SubmitCount != 0 {
		t.Errorf("submit_count = %d, want it untouched", att.SubmitCount)
	}
}

func TestSubmitRejectsProvisioningAndTerminalAttempts(t *testing.T) {
	fr := &fakeRunner{steps: provisioningSteps, block: make(chan struct{})}
	h, _ := newRealHarness(t, fr)

	view, err := h.Start(t.Context(), h.user, h.lab.Id, "real")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := h.Submit(t.Context(), view.Id); !errors.Is(err, ErrProvisioning) {
		t.Fatalf("Submit while provisioning = %v, want ErrProvisioning", err)
	}

	close(fr.block)
	h.waitForStatus(t, store.StatusRunning)
	if _, err := h.Abandon(t.Context(), view.Id); err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if _, err := h.Submit(t.Context(), view.Id); !errors.Is(err, ErrTerminal) {
		t.Fatalf("Submit after the attempt ended = %v, want ErrTerminal", err)
	}
}

func TestSubmitRevealsVisibleCheckpointsOnly(t *testing.T) {
	fr := &fakeRunner{steps: provisioningSteps}
	h, sweeper := newRealHarness(t, fr)
	id := h.startRunningIn(t, "real")

	sweeper.set(map[string]string{"link-up": store.CheckpointPass, "deep-check": store.CheckpointFail})
	result, err := h.Submit(t.Context(), id)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if result.Passed {
		t.Error("passed = true, want false while a hidden checkpoint fails")
	}
	if result.HiddenFailed != 1 || result.SubmitCount != 1 {
		t.Errorf("result = %+v", result)
	}
	if len(result.Checkpoints) != 1 || result.Checkpoints[0].Id != "link-up" {
		t.Fatalf("checkpoints = %+v, want only the visible one", result.Checkpoints)
	}
	if result.Checkpoints[0].Status != store.CheckpointPass || result.Checkpoints[0].Title.Get("en") != "up" {
		t.Errorf("checkpoint = %+v", result.Checkpoints[0])
	}

	ev := h.waitForType(t, EventSubmit)
	if ev.Passed || ev.HiddenFailed != 1 || ev.SubmitCount != 1 {
		t.Errorf("submit event = %+v", ev)
	}
	if att := h.attempt(t, id); att.Status != store.StatusRunning {
		t.Errorf("status = %s, want the attempt to stay running", att.Status)
	}
}

func TestSubmitPassesAndWritesProgressOnce(t *testing.T) {
	fr := &fakeRunner{steps: provisioningSteps}
	h, sweeper := newRealHarness(t, fr)
	id := h.startRunningIn(t, "real")

	if _, err := h.Submit(t.Context(), id); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	sweeper.set(map[string]string{"link-up": store.CheckpointPass, "deep-check": store.CheckpointPass})
	result, err := h.Submit(t.Context(), id)
	if err != nil {
		t.Fatalf("second submit: %v", err)
	}
	if !result.Passed || result.SubmitCount != 2 || result.HiddenFailed != 0 {
		t.Fatalf("result = %+v", result)
	}
	h.waitForStatus(t, store.StatusPassed)

	entries, err := h.store.Progress.ListByUser(t.Context(), h.user)
	if err != nil {
		t.Fatalf("list progress: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("progress = %+v, want one row", entries)
	}
	if entries[0].Kind != store.ProgressLab || entries[0].Ref != h.lab.Id {
		t.Errorf("progress = %+v", entries[0])
	}

	second := h.startRunningIn(t, "real")
	if _, err := h.Submit(t.Context(), second); err != nil {
		t.Fatalf("submit the second attempt: %v", err)
	}
	h.waitForStatus(t, store.StatusPassed)
	entries, err = h.store.Progress.ListByUser(t.Context(), h.user)
	if err != nil {
		t.Fatalf("list progress: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("progress = %+v, want the row to stay unique", entries)
	}
}

func TestMarkDocReadAndProgressFor(t *testing.T) {
	fr := &fakeRunner{steps: provisioningSteps}
	labs, err := content.Load(contenttest.Dir())
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	h := newHarnessFor(t, fr, &content.Content{
		Labs: labs,
		Docs: []content.Doc{{ID: "net/ip/guide", Topic: "net/ip"}},
	})

	if err := h.MarkDocRead(t.Context(), h.user, "net/ip/nope"); !errors.Is(err, ErrUnknownDoc) {
		t.Fatalf("MarkDocRead of an unknown doc = %v, want ErrUnknownDoc", err)
	}
	if err := h.MarkDocRead(t.Context(), h.user, "net/ip/guide"); err != nil {
		t.Fatalf("MarkDocRead: %v", err)
	}
	if err := h.MarkDocRead(t.Context(), h.user, "net/ip/guide"); err != nil {
		t.Fatalf("MarkDocRead twice: %v", err)
	}

	done, err := h.ProgressFor(t.Context(), h.user)
	if err != nil {
		t.Fatalf("ProgressFor: %v", err)
	}
	if len(done) != 1 || !done[ProgressKey{Kind: store.ProgressDoc, Ref: "net/ip/guide"}] {
		t.Errorf("progress = %v", done)
	}
	if done[ProgressKey{Kind: store.ProgressLab, Ref: "net/ip/guide"}] {
		t.Error("a doc was reported as a finished lab")
	}
}
