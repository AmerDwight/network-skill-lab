package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/oklog/ulid/v2"
	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

const (
	dbFileName = "nsl.db"
	timeLayout = "2006-01-02T15:04:05.000Z07:00"
)

type Store struct {
	db   *sql.DB
	path string

	Users          *Users
	Attempts       *Attempts
	CheckpointRuns *CheckpointRuns
	CommandLog     *CommandLog
	Recordings     *Recordings
}

func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir %s: %w", dataDir, err)
	}

	path := filepath.Join(dataDir, dbFileName)
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)",
		path,
	)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping %s: %w", path, err)
	}
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}

	return &Store{
		db:             db,
		path:           path,
		Users:          &Users{db: db},
		Attempts:       &Attempts{db: db},
		CheckpointRuns: &CheckpointRuns{db: db},
		CommandLog:     &CommandLog{db: db},
		Recordings:     &Recordings{db: db},
	}, nil
}

func (s *Store) Path() string { return s.path }

func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close store: %w", err)
	}
	return nil
}

func NewID() string {
	return ulid.Make().String()
}

func formatTime(t time.Time) string {
	return t.UTC().Truncate(time.Millisecond).Format(timeLayout)
}

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func scanNullTime(v sql.NullString) (*time.Time, error) {
	if !v.Valid {
		return nil, nil
	}
	t, err := parseTime(v.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func requireRow(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
