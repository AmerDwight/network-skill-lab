package content

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDocs(t *testing.T) {
	root := writeTree(t, map[string]string{
		"docs/net/ip/guide.zh.md":   "# 介面與路由\n\n內文\n",
		"docs/net/ip/guide.en.md":   "# Interfaces and routing\n\nbody\n",
		"docs/net/dns/guide.zh.md":  "# DNS\n",
		"docs/net/dns/guide.en.md":  "# DNS\n",
		"docs/net/ip/notes.txt":     "ignored\n",
		"docs/net/ip/guide.zh.md.x": "ignored\n",
	})

	docs, err := loadDocs(root)
	if err != nil {
		t.Fatalf("loadDocs() error = %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("loadDocs() returned %d docs, want 2", len(docs))
	}

	doc := docs[1]
	if doc.ID != "net/ip/guide" {
		t.Errorf("ID = %q, want %q", doc.ID, "net/ip/guide")
	}
	if doc.Topic != "net/ip" {
		t.Errorf("Topic = %q, want %q", doc.Topic, "net/ip")
	}
	if doc.Title.Zh != "介面與路由" || doc.Title.En != "Interfaces and routing" {
		t.Errorf("Title = %+v", doc.Title)
	}
	if want := filepath.Join(root, "docs", "net", "ip", "guide.en.md"); doc.Paths["en"] != want {
		t.Errorf("Paths[en] = %q, want %q", doc.Paths["en"], want)
	}

	body, err := doc.Body("zh")
	if err != nil {
		t.Fatalf("Body(zh) error = %v", err)
	}
	if !strings.Contains(body, "內文") {
		t.Errorf("Body(zh) = %q", body)
	}
	if _, err := doc.Body("fr"); err != nil {
		t.Errorf("Body(fr) error = %v, want the English fallback", err)
	}
}

func TestLoadDocsErrors(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  "missing language",
			files: map[string]string{"docs/net/ip/guide.zh.md": "# 指南\n"},
			want:  `doc: "net/ip/guide" has no en version`,
		},
		{
			name: "missing heading",
			files: map[string]string{
				"docs/net/ip/guide.zh.md": "# 指南\n",
				"docs/net/ip/guide.en.md": "no heading here\n",
			},
			want: `title: no "# " heading found`,
		},
		{
			name:  "missing language suffix",
			files: map[string]string{"docs/net/ip/guide.md": "# Guide\n"},
			want:  "name: expected <name>.<zh|en>.md",
		},
		{
			name:  "unsupported language",
			files: map[string]string{"docs/net/ip/guide.fr.md": "# Guide\n"},
			want:  "name: expected <name>.<zh|en>.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadDocs(writeTree(t, tt.files))
			if err == nil {
				t.Fatal("loadDocs() error = nil, want an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("loadDocs() error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestLoadDocsWithoutDirectory(t *testing.T) {
	docs, err := loadDocs(t.TempDir())
	if err != nil {
		t.Fatalf("loadDocs() error = %v", err)
	}
	if docs != nil {
		t.Errorf("loadDocs() = %v, want nil", docs)
	}
}
