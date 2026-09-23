package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

const topicsYAML = `- id: net
  title: { zh: "網路", en: "Networking" }
  children:
    - id: ip
      title: { zh: "介面與路由", en: "Interfaces and routing" }
    - id: dns
      title: { zh: "DNS", en: "DNS" }
`

const tutorialLabYAML = `id: net-ip-02-modes
version: 1
title: { zh: "模式教學", en: "Modes tutorial" }
topic: net/ip
level: 1
modes: [tutorial, guided]
environment: container
estimated_minutes: 5
ticket: { zh: "把 {{iface}} 拉起來", en: "bring {{iface}} up" }
params:
  iface: { gen: const, value: eth1 }
related_docs: [net/ip/guide]
setup: setup.sh
checkpoints:
  - id: link-up
    title: { zh: "{{iface}} 已 UP", en: "{{iface}} is up" }
    node: web01
    script: checks/01-link-up.sh
tutorial:
  - checkpoint: link-up
    instruction: { zh: "執行 ip link set {{iface}} up", en: "run ip link set {{iface}} up" }
solution: { zh: solution.zh.md, en: solution.en.md }
`

const realLabYAML = `id: net-ip-03-submit
version: 1
title: { zh: "提交模式", en: "Submit mode" }
topic: net/ip
level: 2
modes: [guided, real]
environment: container
estimated_minutes: 5
ticket: { zh: "修好 {{iface}}", en: "fix {{iface}}" }
params:
  iface: { gen: const, value: eth1 }
setup: setup.sh
checkpoints:
  - id: link-up
    title: { zh: "{{iface}} 已 UP", en: "{{iface}} is up" }
    node: web01
    script: checks/01-link-up.sh
  - id: deep-check
    title: { zh: "隱藏檢查", en: "hidden check" }
    node: db01
    script: checks/01-link-up.sh
    visible: false
solution: { zh: solution.zh.md, en: solution.en.md }
`

const fixtureTopologyYAML = `nodes:
  web01: { role: ubuntu }
  db01:  { role: ubuntu }
`

const trackYAML = `id: basics
title: { zh: "基礎", en: "Basics" }
steps:
  - doc: net/ip/guide
  - lab: net-ip-03-submit
    mode: guided
`

func writeFile(t *testing.T, path, data string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func writeTopics(t *testing.T, root string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "topics.yaml"), topicsYAML, 0o644)
}

func writeLab(t *testing.T, root, id, lab string) {
	t.Helper()
	dir := filepath.Join(root, "labs", id)
	writeFile(t, filepath.Join(dir, "lab.yaml"), lab, 0o644)
	writeFile(t, filepath.Join(dir, "topology.yaml"), fixtureTopologyYAML, 0o644)
	writeFile(t, filepath.Join(dir, "setup.sh"), "#!/usr/bin/env bash\n", 0o755)
	writeFile(t, filepath.Join(dir, "checks", "01-link-up.sh"), "#!/usr/bin/env bash\n", 0o755)
	writeFile(t, filepath.Join(dir, "solution.zh.md"), "# 解答\n", 0o644)
	writeFile(t, filepath.Join(dir, "solution.en.md"), "# Solution\n", 0o644)
}

func writeFullContent(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeTopics(t, root)
	writeLab(t, root, "net-ip-02-modes", tutorialLabYAML)
	writeLab(t, root, "net-ip-03-submit", realLabYAML)
	writeFile(t, filepath.Join(root, "docs", "net", "ip", "guide.zh.md"), "# 介面指南\n\n用 ip link。\n", 0o644)
	writeFile(t, filepath.Join(root, "docs", "net", "ip", "guide.en.md"), "# Interface guide\n\nUse ip link.\n", 0o644)
	writeFile(t, filepath.Join(root, "tracks", "basics.yaml"), trackYAML, 0o644)
	return root
}

func newContentHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessWithContent(t, writeFullContent(t))
}

func TestListTopics(t *testing.T) {
	h := newContentHarness(t)

	topics := decodeArray(t, h.do(http.MethodGet, "/api/topics", ""))
	if len(topics) != 1 {
		t.Fatalf("topics = %v, want one root", topics)
	}
	root := topics[0].(map[string]any)
	requireKeys(t, root, "id", "title", "labs", "docs", "children")
	if root["id"] != "net" || root["title"] != "網路" {
		t.Errorf("root = %v", root)
	}
	if root["labs"] != float64(2) || root["docs"] != float64(1) {
		t.Errorf("root counts = labs %v docs %v, want the descendants counted", root["labs"], root["docs"])
	}

	children := root["children"].([]any)
	if len(children) != 2 {
		t.Fatalf("children = %v, want two", children)
	}
	ip := children[0].(map[string]any)
	if ip["id"] != "net/ip" || ip["labs"] != float64(2) || ip["docs"] != float64(1) {
		t.Errorf("net/ip = %v", ip)
	}
	dns := children[1].(map[string]any)
	if dns["labs"] != float64(0) || dns["docs"] != float64(0) {
		t.Errorf("net/dns = %v, want empty counts", dns)
	}
	if len(dns["children"].([]any)) != 0 {
		t.Errorf("net/dns children = %v, want an empty array", dns["children"])
	}

	english := decodeArray(t, h.do(http.MethodGet, "/api/topics?lang=en", ""))[0].(map[string]any)
	if english["title"] != "Networking" {
		t.Errorf("title with lang=en = %v", english["title"])
	}
}

func TestListLabsFiltersByTopic(t *testing.T) {
	h := newContentHarness(t)

	all := decodeArray(t, h.do(http.MethodGet, "/api/labs", ""))
	if len(all) != 2 {
		t.Fatalf("labs = %v, want two", all)
	}
	lab := all[0].(map[string]any)
	requireKeys(t, lab, "id", "title", "topic", "level", "modes", "estimated_minutes", "related_docs", "has_hidden_checkpoints")
	related := lab["related_docs"].([]any)
	if len(related) != 1 {
		t.Fatalf("related_docs = %v, want one", related)
	}
	requireKeys(t, related[0].(map[string]any), "id", "title")
	if related[0].(map[string]any)["title"] != "介面指南" {
		t.Errorf("related doc = %v", related[0])
	}
	if lab["has_hidden_checkpoints"] != false {
		t.Errorf("has_hidden_checkpoints = %v, want false", lab["has_hidden_checkpoints"])
	}
	if all[1].(map[string]any)["has_hidden_checkpoints"] != true {
		t.Errorf("the lab with a hidden checkpoint is not flagged")
	}

	byParent := decodeArray(t, h.do(http.MethodGet, "/api/labs?topic=net", ""))
	if len(byParent) != 2 {
		t.Errorf("labs of topic net = %v, want the sub-topics included", byParent)
	}
	byLeaf := decodeArray(t, h.do(http.MethodGet, "/api/labs?topic=net/ip", ""))
	if len(byLeaf) != 2 {
		t.Errorf("labs of topic net/ip = %v", byLeaf)
	}
	if other := decodeArray(t, h.do(http.MethodGet, "/api/labs?topic=net/dns", "")); len(other) != 0 {
		t.Errorf("labs of topic net/dns = %v, want none", other)
	}
}

func TestGetLabCarriesTopicTitle(t *testing.T) {
	h := newContentHarness(t)

	body := decodeJSON(t, h.do(http.MethodGet, "/api/labs/net-ip-03-submit", ""))
	if body["topic_title"] != "介面與路由" {
		t.Errorf("topic_title = %v", body["topic_title"])
	}
	if ids := checkpointIDs(body["checkpoints"].([]any)); len(ids) != 1 || ids[0] != "link-up" {
		t.Errorf("checkpoints = %v, want only the visible one", ids)
	}
}

func TestDocs(t *testing.T) {
	h := newContentHarness(t)

	docs := decodeArray(t, h.do(http.MethodGet, "/api/docs", ""))
	if len(docs) != 1 {
		t.Fatalf("docs = %v, want one", docs)
	}
	requireKeys(t, docs[0].(map[string]any), "id", "title", "topic")
	if docs[0].(map[string]any)["topic"] != "net/ip" {
		t.Errorf("doc = %v", docs[0])
	}

	body := decodeJSON(t, h.do(http.MethodGet, "/api/docs/net/ip/guide", ""))
	requireKeys(t, body, "id", "title", "topic", "body", "completed")
	if body["id"] != "net/ip/guide" || body["completed"] != false {
		t.Errorf("doc detail = %v", body)
	}
	if body["body"] != "# 介面指南\n\n用 ip link。\n" {
		t.Errorf("body = %q", body["body"])
	}

	english := decodeJSON(t, h.do(http.MethodGet, "/api/docs/net/ip/guide?lang=en", ""))
	if english["title"] != "Interface guide" || english["body"] != "# Interface guide\n\nUse ip link.\n" {
		t.Errorf("english doc = %v", english)
	}

	requireError(t, h.do(http.MethodGet, "/api/docs/net/ip/nope", ""), http.StatusNotFound, "not_found")
}

func TestProgressMarksDocsRead(t *testing.T) {
	h := newContentHarness(t)

	resp := h.do(http.MethodPost, "/api/progress", `{"kind":"doc","ref":"net/ip/guide"}`)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if body := decodeJSON(t, h.do(http.MethodGet, "/api/docs/net/ip/guide", "")); body["completed"] != true {
		t.Errorf("completed = %v after marking the doc read", body["completed"])
	}

	requireError(t, h.do(http.MethodPost, "/api/progress", `{"kind":"lab","ref":"net-ip-03-submit"}`), http.StatusBadRequest, "bad_request")
	requireError(t, h.do(http.MethodPost, "/api/progress", `{"kind":"doc","ref":"net/ip/nope"}`), http.StatusNotFound, "not_found")
}

func TestTracks(t *testing.T) {
	h := newContentHarness(t)

	tracks := decodeArray(t, h.do(http.MethodGet, "/api/tracks", ""))
	if len(tracks) != 1 {
		t.Fatalf("tracks = %v, want one", tracks)
	}
	summary := tracks[0].(map[string]any)
	requireKeys(t, summary, "id", "title", "steps", "completed")
	if summary["steps"] != float64(2) || summary["completed"] != float64(0) {
		t.Errorf("track summary = %v", summary)
	}

	detail := decodeJSON(t, h.do(http.MethodGet, "/api/tracks/basics", ""))
	requireKeys(t, detail, "id", "title", "steps")
	steps := detail["steps"].([]any)
	if len(steps) != 2 {
		t.Fatalf("steps = %v, want two", steps)
	}
	doc := steps[0].(map[string]any)
	requireKeys(t, doc, "kind", "ref", "title", "mode", "completed")
	if doc["kind"] != "doc" || doc["ref"] != "net/ip/guide" || doc["title"] != "介面指南" || doc["mode"] != nil {
		t.Errorf("doc step = %v", doc)
	}
	lab := steps[1].(map[string]any)
	if lab["kind"] != "lab" || lab["ref"] != "net-ip-03-submit" || lab["mode"] != "guided" || lab["title"] != "提交模式" {
		t.Errorf("lab step = %v", lab)
	}

	requireError(t, h.do(http.MethodGet, "/api/tracks/nope", ""), http.StatusNotFound, "not_found")
}

func TestTrackStepsFollowProgress(t *testing.T) {
	h := newContentHarness(t)

	if resp := h.do(http.MethodPost, "/api/progress", `{"kind":"doc","ref":"net/ip/guide"}`); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("mark doc read: status = %d", resp.StatusCode)
	}
	id := h.runningLab("net-ip-03-submit", "real")
	h.runner.passChecks()
	if result := h.submit(id); result["passed"] != true {
		t.Fatalf("submit = %v, want passed", result)
	}

	detail := decodeJSON(t, h.do(http.MethodGet, "/api/tracks/basics", ""))
	for _, entry := range detail["steps"].([]any) {
		step := entry.(map[string]any)
		if step["completed"] != true {
			t.Errorf("step %v is not completed", step["ref"])
		}
	}
	summary := decodeArray(t, h.do(http.MethodGet, "/api/tracks", ""))[0].(map[string]any)
	if summary["completed"] != float64(2) {
		t.Errorf("completed = %v, want both steps", summary["completed"])
	}
}
