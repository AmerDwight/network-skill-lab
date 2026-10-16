package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/store"
)

func seedFinishedAttempt(t *testing.T, dir string, endedAgo time.Duration) string {
	t.Helper()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	defer func() { _ = st.Close() }()

	ctx := context.Background()
	user, err := st.Users.Local(ctx)
	if err != nil {
		t.Fatalf("Users.Local() error = %v", err)
	}
	ended := time.Now().Add(-endedAgo)
	att := store.Attempt{
		ID:         store.NewID(),
		UserID:     user.ID,
		LabID:      "net-ip-01-link-down",
		LabVersion: 1,
		Mode:       "guided",
		ParamsJSON: "{}",
		Status:     store.StatusPassed,
		EndedAt:    &ended,
		CreatedAt:  ended.Add(-time.Hour),
	}
	if err := st.Attempts.Create(ctx, att); err != nil {
		t.Fatalf("Attempts.Create() error = %v", err)
	}
	recordings := filepath.Join(dir, "recordings", att.ID)
	if err := os.MkdirAll(recordings, 0o755); err != nil {
		t.Fatalf("create recordings dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(recordings, "web01-main.cast"), []byte("0123456789"), 0o644); err != nil {
		t.Fatalf("write cast: %v", err)
	}
	return att.ID
}

func attemptExists(t *testing.T, dir, id string) bool {
	t.Helper()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	defer func() { _ = st.Close() }()
	_, err = st.Attempts.Get(context.Background(), id)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Attempts.Get() error = %v", err)
	}
	return err == nil
}

func TestPruneDryRunThenForReal(t *testing.T) {
	dir := userDataDir(t)
	old := seedFinishedAttempt(t, dir, 40*24*time.Hour)
	recent := seedFinishedAttempt(t, dir, time.Hour)

	var out bytes.Buffer
	if err := pruneCmd([]string{"--older-than", "30d", "--dry-run"}, &out); err != nil {
		t.Fatalf("prune --dry-run error = %v", err)
	}
	if !strings.Contains(out.String(), old) || !strings.Contains(out.String(), "would remove 1 attempts and free 10 bytes") {
		t.Errorf("dry-run output = %q", out.String())
	}
	if !attemptExists(t, dir, old) {
		t.Fatal("dry run deleted the attempt")
	}

	out.Reset()
	if err := pruneCmd([]string{"--older-than", "30d"}, &out); err != nil {
		t.Fatalf("prune error = %v", err)
	}
	if !strings.Contains(out.String(), "removed 1 attempts and freed 10 bytes") {
		t.Errorf("output = %q", out.String())
	}
	if attemptExists(t, dir, old) {
		t.Error("the old attempt survived the prune")
	}
	if !attemptExists(t, dir, recent) {
		t.Error("the recent attempt was pruned")
	}
	if _, err := os.Stat(filepath.Join(dir, "recordings", old)); !os.IsNotExist(err) {
		t.Errorf("recordings of the old attempt = %v, want them gone", err)
	}
}

func TestPruneRejectsBadArguments(t *testing.T) {
	userDataDir(t)
	for _, args := range [][]string{
		nil,
		{"--older-than", "soon"},
		{"--older-than", "0d"},
		{"--older-than", "30d", "extra"},
	} {
		if err := pruneCmd(args, &bytes.Buffer{}); err == nil {
			t.Errorf("prune %v error = nil, want an error", args)
		}
	}
}
