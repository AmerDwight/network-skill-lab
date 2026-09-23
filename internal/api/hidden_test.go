package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

const hiddenLabYAML = `id: net-hidden-01
version: 1
title: { zh: "隱藏", en: "Hidden" }
topic: net/ip
level: 1
modes: [guided]
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
  - id: gw-forwarding
    title: { zh: "轉送", en: "forwarding" }
    node: web01
    script: checks/01-link-up.sh
    visible: false
solution: { zh: solution.zh.md, en: solution.en.md }
`

const hiddenTopologyYAML = `nodes:
  web01: { role: ubuntu }
  db01:  { role: ubuntu }
`

func writeHiddenLab(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	dir := filepath.Join(root, "labs", "net-hidden-01")
	if err := os.MkdirAll(filepath.Join(dir, "checks"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTopics(t, root)
	files := map[string]struct {
		data string
		mode os.FileMode
	}{
		"lab.yaml":             {hiddenLabYAML, 0o644},
		"topology.yaml":        {hiddenTopologyYAML, 0o644},
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

func checkpointIDs(entries []any) []string {
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.(map[string]any)["id"].(string))
	}
	return ids
}

func TestAttemptPayloadsHideHiddenCheckpoints(t *testing.T) {
	h := newHarnessWithContent(t, writeHiddenLab(t))
	id := h.running()

	for _, path := range []string{"/api/attempts/" + id, "/api/attempts/current"} {
		body := decodeJSON(t, h.do(http.MethodGet, path, ""))
		ids := checkpointIDs(body["checkpoints"].([]any))
		if len(ids) != 1 || ids[0] != "link-up" {
			t.Errorf("%s checkpoints = %v, want only the visible one", path, ids)
		}
	}

	lab := decodeJSON(t, h.do(http.MethodGet, "/api/labs/net-hidden-01", ""))
	if ids := checkpointIDs(lab["checkpoints"].([]any)); len(ids) != 1 || ids[0] != "link-up" {
		t.Errorf("lab checkpoints = %v, want only the visible one", ids)
	}
}

func TestResultPayloadListsEveryCheckpoint(t *testing.T) {
	h := newHarnessWithContent(t, writeHiddenLab(t))
	id := h.running()

	abandoned := decodeJSON(t, h.do(http.MethodPost, "/api/attempts/"+id+"/abandon", ""))
	if ids := checkpointIDs(abandoned["checkpoints"].([]any)); len(ids) != 1 {
		t.Errorf("abandon checkpoints = %v, want only the visible one", ids)
	}

	result := decodeJSON(t, h.do(http.MethodGet, "/api/attempts/"+id+"/result", ""))
	entries := result["checkpoints"].([]any)
	if ids := checkpointIDs(entries); len(ids) != 2 || ids[0] != "link-up" || ids[1] != "gw-forwarding" {
		t.Fatalf("result checkpoints = %v, want both", ids)
	}
	for _, entry := range entries {
		checkpoint := entry.(map[string]any)
		visible, ok := checkpoint["visible"].(bool)
		if !ok {
			t.Fatalf("checkpoint %v has no visible flag", checkpoint)
		}
		if want := checkpoint["id"] == "link-up"; visible != want {
			t.Errorf("checkpoint %v visible = %v, want %v", checkpoint["id"], visible, want)
		}
	}
	requireKeys(t, entries[0].(map[string]any), "id", "title", "status", "first_passed_at", "visible")
}
