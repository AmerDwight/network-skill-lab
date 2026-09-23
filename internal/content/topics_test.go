package content

import (
	"strings"
	"testing"
)

const testTopicsYAML = `- id: net
  title: { zh: "網路", en: "Networking" }
  children:
    - id: ip
      title: { zh: "介面與路由", en: "Interfaces & routing" }
    - id: dns
      title: { zh: "DNS", en: "DNS" }
- id: k3s
  title: { zh: "k3s", en: "k3s" }
  children:
    - id: pods
      title: { zh: "Pod", en: "Pods" }
`

func TestLoadTopics(t *testing.T) {
	root := writeTree(t, map[string]string{"topics.yaml": testTopicsYAML})

	topics, found, err := loadTopics(root)
	if err != nil {
		t.Fatalf("loadTopics() error = %v", err)
	}
	if !found {
		t.Fatal("loadTopics() found = false, want true")
	}
	if len(topics) != 2 {
		t.Fatalf("loadTopics() returned %d roots, want 2", len(topics))
	}

	topic, ok := topics.Find("net/ip")
	if !ok {
		t.Fatal(`Find("net/ip") not found`)
	}
	if topic.Title.Get("en") != "Interfaces & routing" {
		t.Errorf("Title.Get(en) = %q", topic.Title.Get("en"))
	}
	if _, ok := topics.Find("k3s/pods"); !ok {
		t.Error(`Find("k3s/pods") not found`)
	}
	if _, ok := topics.Find("ip"); ok {
		t.Error(`Find("ip") found, child ids must be nested with "/"`)
	}
	if _, ok := topics.Find("net/nope"); ok {
		t.Error(`Find("net/nope") found`)
	}
}

func TestLoadTopicsErrors(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "id contains a slash",
			yaml: "- id: net/ip\n  title: { zh: \"a\", en: \"b\" }\n",
			want: `id must not contain "/"`,
		},
		{
			name: "duplicate id",
			yaml: testTopicsYAML + "- id: net\n  title: { zh: \"a\", en: \"b\" }\n",
			want: `duplicate topic id "net"`,
		},
		{
			name: "missing title",
			yaml: "- id: net\n  title: { zh: \"網路\" }\n",
			want: "topics[net].title.en: must not be empty",
		},
		{
			name: "missing id",
			yaml: "- title: { zh: \"a\", en: \"b\" }\n",
			want: "has no id",
		},
		{
			name: "unknown field",
			yaml: "- id: net\n  title: { zh: \"a\", en: \"b\" }\n  labs: 3\n",
			want: "topics: parse topics.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writeTree(t, map[string]string{"topics.yaml": tt.yaml})
			_, found, err := loadTopics(root)
			if !found {
				t.Fatal("loadTopics() found = false, want true")
			}
			if err == nil {
				t.Fatal("loadTopics() error = nil, want an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("loadTopics() error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestLoadTopicsWithoutFile(t *testing.T) {
	topics, found, err := loadTopics(t.TempDir())
	if err != nil {
		t.Fatalf("loadTopics() error = %v", err)
	}
	if found {
		t.Error("loadTopics() found = true, want false")
	}
	if topics != nil {
		t.Errorf("loadTopics() = %v, want nil", topics)
	}
}
