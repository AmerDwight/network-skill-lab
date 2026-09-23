package recorder

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCastFileFormat(t *testing.T) {
	h := newHarness(t)

	cast, err := h.OpenRecording(t.Context(), h.id, "web01", "1", 120, 40)
	if err != nil {
		t.Fatalf("open recording: %v", err)
	}
	if err := cast.Output([]byte("hello\r\n")); err != nil {
		t.Fatalf("output: %v", err)
	}
	if err := cast.Input([]byte("ip link\n")); err != nil {
		t.Fatalf("input: %v", err)
	}
	if err := cast.Output([]byte{0xff, 0xfe}); err != nil {
		t.Fatalf("output of invalid utf-8: %v", err)
	}
	if err := cast.Resize(100, 30); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if err := cast.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	path := filepath.Join(h.dataDir, "recordings", h.id, "web01-1.cast")
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open cast file: %v", err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatal("cast file is empty")
	}
	var header castHeader
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		t.Fatalf("decode header %q: %v", scanner.Text(), err)
	}
	if header.Version != 2 || header.Width != 120 || header.Height != 40 {
		t.Errorf("header = %+v", header)
	}
	if header.Timestamp <= 0 || header.Env.Term != castTerm || header.Env.Shell != castShell {
		t.Errorf("header = %+v", header)
	}

	var (
		kinds []string
		data  []string
		last  float64
	)
	for scanner.Scan() {
		var event []json.RawMessage
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("decode event %q: %v", scanner.Text(), err)
		}
		if len(event) != 3 {
			t.Fatalf("event %q has %d fields, want 3", scanner.Text(), len(event))
		}
		var at float64
		if err := json.Unmarshal(event[0], &at); err != nil {
			t.Fatalf("decode event time %s: %v", event[0], err)
		}
		if at < last {
			t.Errorf("event time %v is before the previous one %v", at, last)
		}
		last = at

		var kind, payload string
		if err := json.Unmarshal(event[1], &kind); err != nil {
			t.Fatalf("decode event kind %s: %v", event[1], err)
		}
		if err := json.Unmarshal(event[2], &payload); err != nil {
			t.Fatalf("decode event data %s: %v", event[2], err)
		}
		kinds = append(kinds, kind)
		data = append(data, payload)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read cast file: %v", err)
	}

	wantKinds := []string{eventOutput, eventInput, eventOutput, eventResize}
	if len(kinds) != len(wantKinds) {
		t.Fatalf("event kinds = %v, want %v", kinds, wantKinds)
	}
	for i, kind := range wantKinds {
		if kinds[i] != kind {
			t.Errorf("event %d kind = %s, want %s", i, kinds[i], kind)
		}
	}
	if data[0] != "hello\r\n" || data[1] != "ip link\n" || data[3] != "100x30" {
		t.Errorf("event data = %q", data)
	}
	if data[2] != replacement {
		t.Errorf("invalid utf-8 was recorded as %q", data[2])
	}

	recordings, err := h.store.Recordings.ListByAttempt(t.Context(), h.id)
	if err != nil {
		t.Fatalf("list recordings: %v", err)
	}
	if len(recordings) != 1 {
		t.Fatalf("got %d recordings, want 1", len(recordings))
	}
	if recordings[0].Path != path || recordings[0].Node != "web01" || recordings[0].TabID != "1" {
		t.Errorf("recording = %+v", recordings[0])
	}
	if recordings[0].EndedAt == nil {
		t.Error("recording has no ended_at")
	}
}

func TestCastCloseIsIdempotent(t *testing.T) {
	h := newHarness(t)

	cast, err := h.OpenRecording(t.Context(), h.id, "web01", "1", 80, 24)
	if err != nil {
		t.Fatalf("open recording: %v", err)
	}
	if err := cast.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := cast.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if err := cast.Output([]byte("late")); err == nil {
		t.Error("writing to a closed recording succeeded")
	}
}

func TestOpenRecordingRejectsUnsafeNames(t *testing.T) {
	h := newHarness(t)

	if _, err := h.OpenRecording(t.Context(), h.id, "web01", "../escape", 80, 24); err == nil {
		t.Error("a tab name with a path separator was accepted")
	}
}
