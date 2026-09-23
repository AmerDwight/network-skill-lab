package content

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

const labDir = "labs/net-ip-01-link-down/"

func baseFiles() map[string]string {
	return map[string]string{
		"topics.yaml":                     testTopicsYAML,
		"tracks/network-basics.yaml":      testTrackYAML,
		"docs/net/ip/guide.zh.md":         "# 指南\n",
		"docs/net/ip/guide.en.md":         "# Guide\n",
		labDir + "lab.yaml":               validLabYAML,
		labDir + "topology.yaml":          validTopologyYAML,
		labDir + "setup.sh":               "#!/usr/bin/env bash\n",
		labDir + "checks/01-link-up.sh":   "#!/usr/bin/env bash\n",
		labDir + "checks/02-ping-peer.sh": "#!/usr/bin/env bash\n",
		labDir + "solution.zh.md":         "zh\n",
		labDir + "solution.en.md":         "en\n",
	}
}

func TestLoadAll(t *testing.T) {
	all, err := LoadAll(writeTree(t, baseFiles()))
	if err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}
	if len(all.Labs) != 1 {
		t.Errorf("Labs = %d, want 1", len(all.Labs))
	}
	if len(all.Docs) != 1 {
		t.Errorf("Docs = %d, want 1", len(all.Docs))
	}
	if len(all.Topics) != 2 {
		t.Errorf("Topics = %d, want 2", len(all.Topics))
	}
	if len(all.Tracks) != 1 {
		t.Errorf("Tracks = %d, want 1", len(all.Tracks))
	}
}

func TestLoadAllRealContent(t *testing.T) {
	all, err := LoadAll(filepath.Join("..", "..", "content"))
	if err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}
	if len(all.Labs) == 0 {
		t.Error("Labs is empty")
	}
	for _, lab := range all.Labs {
		if _, ok := all.Topics.Find(lab.Topic); !ok {
			t.Errorf("lab %s has topic %q which is not in topics.yaml", lab.Id, lab.Topic)
		}
	}
}

func TestLoadAllEmptyDirectory(t *testing.T) {
	all, err := LoadAll(t.TempDir())
	if err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}
	if len(all.Labs) != 0 || len(all.Docs) != 0 || len(all.Topics) != 0 || len(all.Tracks) != 0 {
		t.Errorf("LoadAll() = %+v, want an empty content set", all)
	}
}

func TestLoadAllCrossReferences(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]string)
		want   []string
	}{
		{
			name: "lab topic is not in the topic tree",
			mutate: func(files map[string]string) {
				files[labDir+"lab.yaml"] = strings.Replace(validLabYAML, "topic: net/ip", "topic: net/nope", 1)
			},
			want: []string{`topic: "net/nope" is not in topics.yaml`},
		},
		{
			name: "doc topic is not in the topic tree",
			mutate: func(files map[string]string) {
				files["docs/net/nope/guide.zh.md"] = "# 指南\n"
				files["docs/net/nope/guide.en.md"] = "# Guide\n"
			},
			want: []string{`topic: "net/nope" is not in topics.yaml`},
		},
		{
			name: "doc outside a topic directory",
			mutate: func(files map[string]string) {
				files["docs/guide.zh.md"] = "# 指南\n"
				files["docs/guide.en.md"] = "# Guide\n"
			},
			want: []string{"topic: a doc must live in a topic directory"},
		},
		{
			name: "related doc does not exist",
			mutate: func(files map[string]string) {
				files[labDir+"lab.yaml"] = validLabYAML + "related_docs: [net/ip/guide, net/ip/nope]\n"
			},
			want: []string{`related_docs[1]: "net/ip/nope" is not a known doc`},
		},
		{
			name: "track step references a missing doc",
			mutate: func(files map[string]string) {
				files["tracks/network-basics.yaml"] = strings.Replace(testTrackYAML, "doc: net/ip/guide", "doc: net/ip/nope", 1)
			},
			want: []string{`steps[0]: doc "net/ip/nope" does not exist`},
		},
		{
			name: "track step references a missing lab",
			mutate: func(files map[string]string) {
				files["tracks/network-basics.yaml"] = strings.Replace(testTrackYAML, "lab: net-ip-01-link-down", "lab: net-ip-99-nope", 1)
			},
			want: []string{`steps[1]: lab "net-ip-99-nope" does not exist`},
		},
		{
			name: "track step uses a mode the lab does not support",
			mutate: func(files map[string]string) {
				files["tracks/network-basics.yaml"] = strings.Replace(testTrackYAML, "mode: guided", "mode: real", 1)
			},
			want: []string{`steps[1]: lab "net-ip-01-link-down" does not support mode "real"`},
		},
		{
			name:   "topics.yaml is missing",
			mutate: func(files map[string]string) { delete(files, "topics.yaml") },
			want:   []string{"topics: file is required when the content directory has labs or docs"},
		},
		{
			name: "every problem is reported",
			mutate: func(files map[string]string) {
				files[labDir+"lab.yaml"] = strings.Replace(validLabYAML, "topic: net/ip", "topic: net/nope", 1) +
					"related_docs: [net/ip/nope]\n"
				files["tracks/network-basics.yaml"] = strings.Replace(testTrackYAML, "doc: net/ip/guide", "doc: net/ip/nope", 1)
			},
			want: []string{
				`topic: "net/nope" is not in topics.yaml`,
				`related_docs[0]: "net/ip/nope" is not a known doc`,
				`steps[0]: doc "net/ip/nope" does not exist`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := baseFiles()
			tt.mutate(files)
			_, err := LoadAll(writeTree(t, files))
			if err == nil {
				t.Fatal("LoadAll() error = nil, want an error")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("LoadAll() error = %q, want it to contain %q", err, want)
				}
			}
		})
	}
}

func TestLoadAllAcceptsKnownReferences(t *testing.T) {
	files := baseFiles()
	files[labDir+"lab.yaml"] = validLabYAML + "related_docs: [net/ip/guide]\n"

	all, err := LoadAll(writeTree(t, files))
	if err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}
	if len(all.Labs[0].RelatedDocs) != 1 {
		t.Errorf("RelatedDocs = %v, want one entry", all.Labs[0].RelatedDocs)
	}
}

func TestValidateRefsDuplicateDocID(t *testing.T) {
	all := &Content{Docs: []Doc{
		{ID: "net/ip/guide", Topic: "net/ip", Paths: map[string]string{"zh": "a.zh.md"}},
		{ID: "net/ip/guide", Topic: "net/ip", Paths: map[string]string{"zh": "b.zh.md"}},
	}}

	errs := validateRefs(all)
	found := false
	for _, err := range errs {
		found = found || strings.Contains(err.Error(), `id: duplicate doc id "net/ip/guide"`)
	}
	if !found {
		t.Errorf("validateRefs() = %v, want a duplicate doc id error", errs)
	}
}
