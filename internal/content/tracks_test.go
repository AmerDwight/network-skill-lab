package content

import (
	"strings"
	"testing"
)

const testTrackYAML = `id: network-basics
title: { zh: "網路基礎排查", en: "Network troubleshooting basics" }
steps:
  - { doc: net/ip/guide }
  - { lab: net-ip-01-link-down, mode: guided }
`

func TestLoadTracks(t *testing.T) {
	root := writeTree(t, map[string]string{
		"tracks/network-basics.yaml": testTrackYAML,
		"tracks/notes.md":            "ignored\n",
	})

	tracks, err := loadTracks(root)
	if err != nil {
		t.Fatalf("loadTracks() error = %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("loadTracks() returned %d tracks, want 1", len(tracks))
	}

	track := tracks[0]
	if track.ID != "network-basics" {
		t.Errorf("ID = %q", track.ID)
	}
	if track.Title.Get("zh") != "網路基礎排查" {
		t.Errorf("Title.Get(zh) = %q", track.Title.Get("zh"))
	}
	want := []TrackStep{{Doc: "net/ip/guide"}, {Lab: "net-ip-01-link-down", Mode: "guided"}}
	if len(track.Steps) != len(want) || track.Steps[0] != want[0] || track.Steps[1] != want[1] {
		t.Errorf("Steps = %+v, want %+v", track.Steps, want)
	}
}

func TestLoadTracksErrors(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "id does not match file name",
			yaml: strings.Replace(testTrackYAML, "id: network-basics", "id: other", 1),
			want: `id: "other" does not match file name "network-basics"`,
		},
		{
			name: "step with both doc and lab",
			yaml: strings.Replace(testTrackYAML, "- { doc: net/ip/guide }", "- { doc: net/ip/guide, lab: net-ip-01-link-down, mode: guided }", 1),
			want: "steps[0]: must set either doc or lab, not both",
		},
		{
			name: "step with neither doc nor lab",
			yaml: strings.Replace(testTrackYAML, "- { doc: net/ip/guide }", "- { mode: guided }", 1),
			want: "steps[0]: must set either doc or lab",
		},
		{
			name: "lab step without mode",
			yaml: strings.Replace(testTrackYAML, "- { lab: net-ip-01-link-down, mode: guided }", "- { lab: net-ip-01-link-down }", 1),
			want: "steps[1]: a lab step must set mode",
		},
		{
			name: "doc step with mode",
			yaml: strings.Replace(testTrackYAML, "- { doc: net/ip/guide }", "- { doc: net/ip/guide, mode: guided }", 1),
			want: "steps[0]: mode is only valid for lab steps",
		},
		{
			name: "no steps",
			yaml: "id: network-basics\ntitle: { zh: \"a\", en: \"b\" }\nsteps: []\n",
			want: "steps: must not be empty",
		},
		{
			name: "missing title",
			yaml: strings.Replace(testTrackYAML, `title: { zh: "網路基礎排查", en: "Network troubleshooting basics" }`, `title: { zh: "網路基礎排查" }`, 1),
			want: "title.en: must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writeTree(t, map[string]string{"tracks/network-basics.yaml": tt.yaml})
			_, err := loadTracks(root)
			if err == nil {
				t.Fatal("loadTracks() error = nil, want an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("loadTracks() error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestLoadTracksWithoutDirectory(t *testing.T) {
	tracks, err := loadTracks(t.TempDir())
	if err != nil {
		t.Fatalf("loadTracks() error = %v", err)
	}
	if tracks != nil {
		t.Errorf("loadTracks() = %v, want nil", tracks)
	}
}
