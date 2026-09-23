package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s
}

func newAttempt(t *testing.T, s *Store, status string) Attempt {
	t.Helper()
	user, err := s.Users.Local(context.Background())
	if err != nil {
		t.Fatalf("Users.Local: %v", err)
	}
	attempt := Attempt{
		ID:         NewID(),
		UserID:     user.ID,
		LabID:      "vlan-basics",
		LabVersion: 1,
		CaseID:     "case-a",
		Mode:       "guided",
		ParamsJSON: `{"vlan":10}`,
		Status:     status,
		CreatedAt:  time.Now(),
	}
	if err := s.Attempts.Create(context.Background(), attempt); err != nil {
		t.Fatalf("Attempts.Create: %v", err)
	}
	return attempt
}

func TestOpenCreatesDataDirAndIsIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "nsl.db")); err != nil {
		t.Fatalf("stat db file: %v", err)
	}
	if s.Path() != filepath.Join(dir, "nsl.db") {
		t.Fatalf("Path() = %q", s.Path())
	}
	attempt := newAttempt(t, s, StatusRunning)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	var applied int
	if err := reopened.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if want := len(mustLoadMigrations(t)); applied != want {
		t.Fatalf("applied migrations = %d, want %d", applied, want)
	}
	if _, err := reopened.Attempts.Get(context.Background(), attempt.ID); err != nil {
		t.Fatalf("attempt lost across reopen: %v", err)
	}

	var journalMode string
	if err := reopened.db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatalf("pragma journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}
}

func mustLoadMigrations(t *testing.T) []migration {
	t.Helper()
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("no migrations embedded")
	}
	return migrations
}

func TestLocalUserIsSeeded(t *testing.T) {
	s := openTestStore(t)

	user, err := s.Users.Local(context.Background())
	if err != nil {
		t.Fatalf("Users.Local: %v", err)
	}
	if user.Username != LocalUsername {
		t.Fatalf("Username = %q", user.Username)
	}
	if user.Role != RoleUser {
		t.Fatalf("Role = %q", user.Role)
	}
	if user.PasswordHash != "" {
		t.Fatalf("PasswordHash = %q, want empty", user.PasswordHash)
	}
	if user.Locale != "zh" {
		t.Fatalf("Locale = %q", user.Locale)
	}
	if _, err := ulid.Parse(user.ID); err != nil {
		t.Fatalf("seed id %q is not a ULID: %v", user.ID, err)
	}
	if user.CreatedAt.IsZero() || user.CreatedAt.Location() != time.UTC {
		t.Fatalf("CreatedAt = %v", user.CreatedAt)
	}
}

func TestAttemptRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	attempt := newAttempt(t, s, StatusProvisioning)

	got, err := s.Attempts.Get(ctx, attempt.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LabID != attempt.LabID || got.CaseID != attempt.CaseID || got.Mode != attempt.Mode {
		t.Fatalf("Get = %+v", got)
	}
	if got.ParamsJSON != attempt.ParamsJSON || got.LabVersion != 1 || got.ElapsedMS != 0 {
		t.Fatalf("Get = %+v", got)
	}
	if got.StartedAt != nil || got.EndedAt != nil || got.ErrorMessage != "" {
		t.Fatalf("Get = %+v", got)
	}

	startedAt := time.Now().Add(-time.Minute)
	if err := s.Attempts.SetStartedAt(ctx, attempt.ID, startedAt); err != nil {
		t.Fatalf("SetStartedAt: %v", err)
	}
	if err := s.Attempts.SetSandbox(ctx, attempt.ID, "local", "sbx-1"); err != nil {
		t.Fatalf("SetSandbox: %v", err)
	}
	if err := s.Attempts.SetElapsed(ctx, attempt.ID, 90*time.Second); err != nil {
		t.Fatalf("SetElapsed: %v", err)
	}
	if err := s.Attempts.SetStatus(ctx, attempt.ID, StatusRunning, nil, ""); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	got, err = s.Attempts.Get(ctx, attempt.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != StatusRunning || got.RunnerID != "local" || got.SandboxID != "sbx-1" {
		t.Fatalf("Get = %+v", got)
	}
	if got.ElapsedMS != 90000 {
		t.Fatalf("ElapsedMS = %d", got.ElapsedMS)
	}
	if got.StartedAt == nil || !got.StartedAt.Equal(startedAt.UTC().Truncate(time.Millisecond)) {
		t.Fatalf("StartedAt = %v, want %v", got.StartedAt, startedAt)
	}

	endedAt := time.Now()
	if err := s.Attempts.SetStatus(ctx, attempt.ID, StatusError, &endedAt, "provision failed"); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	got, err = s.Attempts.Get(ctx, attempt.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != StatusError || got.ErrorMessage != "provision failed" {
		t.Fatalf("Get = %+v", got)
	}
	if got.EndedAt == nil || !got.EndedAt.Equal(endedAt.UTC().Truncate(time.Millisecond)) {
		t.Fatalf("EndedAt = %v, want %v", got.EndedAt, endedAt)
	}
}

func TestAttemptNotFound(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	if _, err := s.Attempts.Get(ctx, NewID()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing = %v, want ErrNotFound", err)
	}
	if err := s.Attempts.SetStatus(ctx, NewID(), StatusPassed, nil, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetStatus missing = %v, want ErrNotFound", err)
	}
	if err := s.Attempts.SetElapsed(ctx, NewID(), time.Second); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetElapsed missing = %v, want ErrNotFound", err)
	}
	if err := s.Attempts.SetSandbox(ctx, NewID(), "local", "sbx"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetSandbox missing = %v, want ErrNotFound", err)
	}
	if err := s.Attempts.SetStartedAt(ctx, NewID(), time.Now()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetStartedAt missing = %v, want ErrNotFound", err)
	}
}

func TestActiveForUserAndListNonTerminal(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	user, err := s.Users.Local(ctx)
	if err != nil {
		t.Fatalf("Users.Local: %v", err)
	}

	if _, ok, err := s.Attempts.ActiveForUser(ctx, user.ID); err != nil || ok {
		t.Fatalf("ActiveForUser on empty store = ok %v, err %v", ok, err)
	}

	done := newAttempt(t, s, StatusPassed)
	active := newAttempt(t, s, StatusRunning)

	got, ok, err := s.Attempts.ActiveForUser(ctx, user.ID)
	if err != nil || !ok {
		t.Fatalf("ActiveForUser = ok %v, err %v", ok, err)
	}
	if got.ID != active.ID {
		t.Fatalf("ActiveForUser = %s, want %s", got.ID, active.ID)
	}

	nonTerminal, err := s.Attempts.ListNonTerminal(ctx)
	if err != nil {
		t.Fatalf("ListNonTerminal: %v", err)
	}
	if len(nonTerminal) != 1 || nonTerminal[0].ID != active.ID {
		t.Fatalf("ListNonTerminal = %+v", nonTerminal)
	}

	provisioning := newAttempt(t, s, StatusProvisioning)
	nonTerminal, err = s.Attempts.ListNonTerminal(ctx)
	if err != nil {
		t.Fatalf("ListNonTerminal: %v", err)
	}
	if len(nonTerminal) != 2 {
		t.Fatalf("ListNonTerminal = %+v", nonTerminal)
	}

	for _, id := range []string{active.ID, provisioning.ID} {
		if err := s.Attempts.SetStatus(ctx, id, StatusAbandoned, nil, ""); err != nil {
			t.Fatalf("SetStatus: %v", err)
		}
	}
	if _, ok, err := s.Attempts.ActiveForUser(ctx, user.ID); err != nil || ok {
		t.Fatalf("ActiveForUser after abandon = ok %v, err %v", ok, err)
	}
	nonTerminal, err = s.Attempts.ListNonTerminal(ctx)
	if err != nil {
		t.Fatalf("ListNonTerminal: %v", err)
	}
	if len(nonTerminal) != 0 {
		t.Fatalf("ListNonTerminal = %+v", nonTerminal)
	}
	if _, err := s.Attempts.Get(ctx, done.ID); err != nil {
		t.Fatalf("Get finished attempt: %v", err)
	}
}

func TestCheckpointUpsertKeepsFirstPassedAt(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	attempt := newAttempt(t, s, StatusRunning)

	base := time.Now().Truncate(time.Millisecond)
	steps := []struct {
		status string
		at     time.Time
	}{
		{CheckpointFail, base},
		{CheckpointPass, base.Add(time.Second)},
		{CheckpointFail, base.Add(2 * time.Second)},
		{CheckpointPass, base.Add(3 * time.Second)},
	}
	for _, step := range steps {
		at := step.at
		if err := s.CheckpointRuns.Upsert(ctx, CheckpointRun{
			AttemptID:    attempt.ID,
			CheckpointID: "cp1",
			LastStatus:   step.status,
			LastRunAt:    &at,
		}); err != nil {
			t.Fatalf("Upsert %s: %v", step.status, err)
		}
	}

	runs, err := s.CheckpointRuns.ListByAttempt(ctx, attempt.ID)
	if err != nil {
		t.Fatalf("ListByAttempt: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("ListByAttempt = %+v", runs)
	}
	run := runs[0]
	if run.LastStatus != CheckpointPass {
		t.Fatalf("LastStatus = %q", run.LastStatus)
	}
	if run.LastRunAt == nil || !run.LastRunAt.Equal(base.Add(3*time.Second).UTC()) {
		t.Fatalf("LastRunAt = %v", run.LastRunAt)
	}
	if run.FirstPassedAt == nil || !run.FirstPassedAt.Equal(base.Add(time.Second).UTC()) {
		t.Fatalf("FirstPassedAt = %v, want %v", run.FirstPassedAt, base.Add(time.Second).UTC())
	}
}

func TestCheckpointUpsertLeavesFirstPassedAtNullUntilPass(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	attempt := newAttempt(t, s, StatusRunning)

	at := time.Now()
	if err := s.CheckpointRuns.Upsert(ctx, CheckpointRun{
		AttemptID:    attempt.ID,
		CheckpointID: "cp1",
		LastStatus:   CheckpointPending,
		LastRunAt:    &at,
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	runs, err := s.CheckpointRuns.ListByAttempt(ctx, attempt.ID)
	if err != nil {
		t.Fatalf("ListByAttempt: %v", err)
	}
	if len(runs) != 1 || runs[0].FirstPassedAt != nil {
		t.Fatalf("ListByAttempt = %+v", runs)
	}
}

func TestCommandLogBatchCountAndOrder(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	attempt := newAttempt(t, s, StatusRunning)
	other := newAttempt(t, s, StatusRunning)

	base := time.Now().Truncate(time.Millisecond)
	entries := []CommandEntry{
		{AttemptID: attempt.ID, Node: "r1", TS: base.Add(2 * time.Second), User: "root", CWD: "/root", Command: "ip addr", ExitCode: 0},
		{AttemptID: attempt.ID, Node: "r1", TS: base, User: "root", CWD: "/root", Command: "ping -c1 10.0.0.2", ExitCode: 1},
		{AttemptID: attempt.ID, Node: "r2", TS: base.Add(time.Second), User: "root", CWD: "/", Command: "ip route", ExitCode: 0},
	}
	if err := s.CommandLog.AppendBatch(ctx, entries); err != nil {
		t.Fatalf("AppendBatch: %v", err)
	}
	if err := s.CommandLog.AppendBatch(ctx, nil); err != nil {
		t.Fatalf("AppendBatch empty: %v", err)
	}

	count, err := s.CommandLog.CountByAttempt(ctx, attempt.ID)
	if err != nil {
		t.Fatalf("CountByAttempt: %v", err)
	}
	if count != 3 {
		t.Fatalf("CountByAttempt = %d", count)
	}
	if count, err := s.CommandLog.CountByAttempt(ctx, other.ID); err != nil || count != 0 {
		t.Fatalf("CountByAttempt other = %d, err %v", count, err)
	}

	got, err := s.CommandLog.ListByAttempt(ctx, attempt.ID)
	if err != nil {
		t.Fatalf("ListByAttempt: %v", err)
	}
	wantCommands := []string{"ping -c1 10.0.0.2", "ip route", "ip addr"}
	if len(got) != len(wantCommands) {
		t.Fatalf("ListByAttempt = %+v", got)
	}
	for i, want := range wantCommands {
		if got[i].Command != want {
			t.Fatalf("entry %d command = %q, want %q", i, got[i].Command, want)
		}
		if got[i].ID == 0 {
			t.Fatalf("entry %d has no id", i)
		}
	}
	if got[0].ExitCode != 1 || got[0].User != "root" || got[0].Node != "r1" || got[0].CWD != "/root" {
		t.Fatalf("entry 0 = %+v", got[0])
	}
	if !got[0].TS.Equal(base.UTC()) {
		t.Fatalf("entry 0 ts = %v, want %v", got[0].TS, base.UTC())
	}
}

func TestRecordingsRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	attempt := newAttempt(t, s, StatusRunning)

	startedAt := time.Now().Truncate(time.Millisecond)
	rec := Recording{
		ID:        NewID(),
		AttemptID: attempt.ID,
		Node:      "r1",
		TabID:     "tab-1",
		Path:      "recordings/" + attempt.ID + "/r1-tab-1.cast",
		StartedAt: startedAt,
	}
	if err := s.Recordings.Create(ctx, rec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Recordings.ListByAttempt(ctx, attempt.ID)
	if err != nil {
		t.Fatalf("ListByAttempt: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListByAttempt = %+v", got)
	}
	if got[0].Path != rec.Path || got[0].Node != rec.Node || got[0].TabID != rec.TabID {
		t.Fatalf("ListByAttempt = %+v", got[0])
	}
	if got[0].EndedAt != nil {
		t.Fatalf("EndedAt = %v, want nil", got[0].EndedAt)
	}
	if !got[0].StartedAt.Equal(startedAt.UTC()) {
		t.Fatalf("StartedAt = %v, want %v", got[0].StartedAt, startedAt.UTC())
	}

	endedAt := startedAt.Add(5 * time.Minute)
	if err := s.Recordings.SetEnded(ctx, rec.ID, endedAt); err != nil {
		t.Fatalf("SetEnded: %v", err)
	}
	if err := s.Recordings.SetEnded(ctx, NewID(), endedAt); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetEnded missing = %v, want ErrNotFound", err)
	}

	got, err = s.Recordings.ListByAttempt(ctx, attempt.ID)
	if err != nil {
		t.Fatalf("ListByAttempt: %v", err)
	}
	if got[0].EndedAt == nil || !got[0].EndedAt.Equal(endedAt.UTC()) {
		t.Fatalf("EndedAt = %v, want %v", got[0].EndedAt, endedAt.UTC())
	}
}

func TestDeletingAttemptCascades(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	attempt := newAttempt(t, s, StatusRunning)

	now := time.Now()
	if err := s.CheckpointRuns.Upsert(ctx, CheckpointRun{
		AttemptID: attempt.ID, CheckpointID: "cp1", LastStatus: CheckpointPass, LastRunAt: &now,
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := s.CommandLog.AppendBatch(ctx, []CommandEntry{
		{AttemptID: attempt.ID, Node: "r1", TS: now, Command: "ip addr"},
	}); err != nil {
		t.Fatalf("AppendBatch: %v", err)
	}
	if err := s.Recordings.Create(ctx, Recording{
		ID: NewID(), AttemptID: attempt.ID, Node: "r1", TabID: "tab-1", Path: "x.cast", StartedAt: now,
	}); err != nil {
		t.Fatalf("Create recording: %v", err)
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM attempts WHERE id = ?`, attempt.ID); err != nil {
		t.Fatalf("delete attempt: %v", err)
	}

	for _, table := range []string{"checkpoint_runs", "command_log", "recordings"} {
		var count int
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM `+table+` WHERE attempt_id = ?`, attempt.ID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s still has %d rows after cascade", table, count)
		}
	}
}

func TestForeignKeyIsEnforced(t *testing.T) {
	s := openTestStore(t)

	err := s.Attempts.Create(context.Background(), Attempt{
		ID: NewID(), UserID: NewID(), LabID: "lab", LabVersion: 1,
		Mode: "guided", ParamsJSON: "{}", Status: StatusProvisioning, CreatedAt: time.Now(),
	})
	if err == nil {
		t.Fatal("Create with unknown user_id succeeded, want foreign key violation")
	}
}

func TestTimestampsSurviveRoundTripAtMillisecondPrecision(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	loc := time.FixedZone("UTC+8", 8*3600)
	startedAt := time.Date(2026, 3, 4, 5, 6, 7, 123_456_789, loc)
	want := startedAt.UTC().Truncate(time.Millisecond)

	attempt := newAttempt(t, s, StatusRunning)
	if err := s.Attempts.SetStartedAt(ctx, attempt.ID, startedAt); err != nil {
		t.Fatalf("SetStartedAt: %v", err)
	}

	got, err := s.Attempts.Get(ctx, attempt.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.StartedAt == nil || !got.StartedAt.Equal(want) {
		t.Fatalf("StartedAt = %v, want %v", got.StartedAt, want)
	}
	if got.StartedAt.Location() != time.UTC {
		t.Fatalf("StartedAt location = %v, want UTC", got.StartedAt.Location())
	}
	if got.StartedAt.Nanosecond() != 123_000_000 {
		t.Fatalf("StartedAt nanosecond = %d, want 123000000", got.StartedAt.Nanosecond())
	}

	var stored string
	if err := s.db.QueryRowContext(ctx, `SELECT started_at FROM attempts WHERE id = ?`, attempt.ID).Scan(&stored); err != nil {
		t.Fatalf("select started_at: %v", err)
	}
	if stored != "2026-03-03T21:06:07.123Z" {
		t.Fatalf("stored started_at = %q", stored)
	}
}

func TestNewIDIsMonotonicULID(t *testing.T) {
	previous := ""
	for range 100 {
		id := NewID()
		if _, err := ulid.Parse(id); err != nil {
			t.Fatalf("NewID returned %q: %v", id, err)
		}
		if id <= previous {
			t.Fatalf("NewID returned %q after %q, want increasing", id, previous)
		}
		previous = id
	}
}
