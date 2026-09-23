package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

const localUserID = "01K5S3J8XQZ4NV7B0WGDHM2RCT"

func openAtVersion1(t *testing.T, dir string) {
	t.Helper()

	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, dbFileName))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := t.Context()
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	first, err := migrationsFS.ReadFile("migrations/0001_init.sql")
	if err != nil {
		t.Fatalf("read 0001: %v", err)
	}
	if err := applyMigration(ctx, db, migration{version: 1, name: "0001_init.sql", sql: string(first)}); err != nil {
		t.Fatalf("apply 0001: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO attempts (id, user_id, lab_id, lab_version, mode, params_json, status, elapsed_ms, created_at)
		VALUES ('att1', ?, 'net-ip-01-link-down', 1, 'guided', '{"iface":"eth1"}', 'passed', 1200, ?)`,
		localUserID, formatTime(time.Now())); err != nil {
		t.Fatalf("insert attempt: %v", err)
	}
}

func TestMigration0002UpgradesAVersion1Database(t *testing.T) {
	dir := t.TempDir()
	openAtVersion1(t, dir)

	st, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	applied, err := appliedVersions(t.Context(), st.db)
	if err != nil {
		t.Fatalf("appliedVersions() error = %v", err)
	}
	if !applied[1] || !applied[2] {
		t.Fatalf("applied versions = %v, want 1 and 2", applied)
	}

	att, err := st.Attempts.Get(t.Context(), "att1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if att.SubmitCount != 0 {
		t.Errorf("submit count = %d, want 0", att.SubmitCount)
	}
	if att.Seed != nil {
		t.Errorf("seed = %v, want nil", *att.Seed)
	}
	if att.ParamsJSON != `{"iface":"eth1"}` {
		t.Errorf("params were lost: %q", att.ParamsJSON)
	}
}

func TestAttemptSeedAndSubmitCountRoundTrip(t *testing.T) {
	st := openStore(t)
	ctx := t.Context()

	seed := int64(-42)
	att := Attempt{
		ID:         NewID(),
		UserID:     localUser(t, st).ID,
		LabID:      "net-ip-01-link-down",
		LabVersion: 1,
		CaseID:     "default",
		Mode:       "guided",
		ParamsJSON: "{}",
		Seed:       &seed,
		Status:     StatusProvisioning,
		CreatedAt:  time.Now(),
	}
	if err := st.Attempts.Create(ctx, att); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	stored, err := st.Attempts.Get(ctx, att.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.Seed == nil || *stored.Seed != seed {
		t.Fatalf("seed = %v, want %d", stored.Seed, seed)
	}
	if stored.CaseID != "default" {
		t.Errorf("case id = %q", stored.CaseID)
	}

	if err := st.Attempts.SetResolved(ctx, att.ID, 7, "wrong-mtu", `{"fault":"mtu"}`); err != nil {
		t.Fatalf("SetResolved() error = %v", err)
	}
	stored, err = st.Attempts.Get(ctx, att.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.Seed == nil || *stored.Seed != 7 || stored.CaseID != "wrong-mtu" || stored.ParamsJSON != `{"fault":"mtu"}` {
		t.Errorf("attempt after SetResolved = %+v", stored)
	}

	for range 3 {
		if err := st.Attempts.IncrementSubmitCount(ctx, att.ID); err != nil {
			t.Fatalf("IncrementSubmitCount() error = %v", err)
		}
	}
	stored, err = st.Attempts.Get(ctx, att.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.SubmitCount != 3 {
		t.Errorf("submit count = %d, want 3", stored.SubmitCount)
	}
}

func TestProgressUpsertAndList(t *testing.T) {
	st := openStore(t)
	ctx := t.Context()
	user := localUser(t, st).ID

	if err := st.Progress.Upsert(ctx, user, ProgressLab, "net-ip-01-link-down"); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if err := st.Progress.Upsert(ctx, user, ProgressLab, "net-ip-01-link-down"); err != nil {
		t.Fatalf("Upsert() twice error = %v", err)
	}
	if err := st.Progress.Upsert(ctx, user, ProgressDoc, "net/ip/guide"); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	entries, err := st.Progress.ListByUser(ctx, user)
	if err != nil {
		t.Fatalf("ListByUser() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Kind != ProgressDoc || entries[0].Ref != "net/ip/guide" {
		t.Errorf("entries[0] = %+v", entries[0])
	}
	if entries[1].Kind != ProgressLab || entries[1].Ref != "net-ip-01-link-down" {
		t.Errorf("entries[1] = %+v", entries[1])
	}
	if entries[0].CompletedAt.IsZero() {
		t.Error("completed_at was not stored")
	}

	empty, err := st.Progress.ListByUser(ctx, "nobody")
	if err != nil {
		t.Fatalf("ListByUser() error = %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("got %d entries for an unknown user", len(empty))
	}
}

func TestProgressRejectsAnUnknownKind(t *testing.T) {
	st := openStore(t)
	if err := st.Progress.Upsert(t.Context(), localUser(t, st).ID, "video", "x"); err == nil {
		t.Fatal("Upsert() accepted an unknown kind")
	}
}

func openStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func localUser(t *testing.T, st *Store) User {
	t.Helper()
	user, err := st.Users.Local(context.Background())
	if err != nil {
		t.Fatalf("Local() error = %v", err)
	}
	return user
}
