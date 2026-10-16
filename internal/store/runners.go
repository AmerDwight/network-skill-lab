package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	sqlite "modernc.org/sqlite"
)

var ErrRunnerNameTaken = errors.New("runner name already taken")

type Runners struct {
	db *sql.DB
}

const runnerColumns = `id, name, token_hash, capabilities, capacity, created_at, last_seen_at, revoked_at, version`

func (r *Runners) Create(ctx context.Context, runner Runner) error {
	if runner.CreatedAt.IsZero() {
		runner.CreatedAt = time.Now()
	}
	capabilities, err := encodeCapabilities(runner.Capabilities)
	if err != nil {
		return fmt.Errorf("create runner %s: %w", runner.Name, err)
	}

	_, err = r.db.ExecContext(ctx,
		`INSERT INTO runners (`+runnerColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		runner.ID,
		runner.Name,
		runner.TokenHash,
		capabilities,
		runner.Capacity,
		formatTime(runner.CreatedAt),
		nullTime(runner.LastSeenAt),
		nullTime(runner.RevokedAt),
		nullString(runner.Version),
	)
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) && sqliteErr.Code() == sqliteConstraintUnique {
		return fmt.Errorf("create runner %s: %w", runner.Name, ErrRunnerNameTaken)
	}
	if err != nil {
		return fmt.Errorf("create runner %s: %w", runner.Name, err)
	}
	return nil
}

func (r *Runners) ByName(ctx context.Context, name string) (Runner, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+runnerColumns+` FROM runners WHERE name = ?`, name)
	runner, err := scanRunner(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Runner{}, fmt.Errorf("runner %s: %w", name, ErrNotFound)
	}
	if err != nil {
		return Runner{}, fmt.Errorf("runner %s: %w", name, err)
	}
	return runner, nil
}

func (r *Runners) List(ctx context.Context) ([]Runner, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+runnerColumns+` FROM runners ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list runners: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var runners []Runner
	for rows.Next() {
		runner, err := scanRunner(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("list runners: %w", err)
		}
		runners = append(runners, runner)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list runners: %w", err)
	}
	return runners, nil
}

func (r *Runners) Touch(ctx context.Context, id string, lastSeen time.Time, version string, capacity int, capabilities []string) error {
	encoded, err := encodeCapabilities(capabilities)
	if err != nil {
		return fmt.Errorf("touch runner %s: %w", id, err)
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE runners SET last_seen_at = ?, version = ?, capacity = ?, capabilities = ? WHERE id = ?`,
		formatTime(lastSeen), nullString(version), capacity, encoded, id)
	if err != nil {
		return fmt.Errorf("touch runner %s: %w", id, err)
	}
	if err := requireRow(res); err != nil {
		return fmt.Errorf("touch runner %s: %w", id, err)
	}
	return nil
}

func (r *Runners) Revoke(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE runners SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?`,
		formatTime(time.Now()), id)
	if err != nil {
		return fmt.Errorf("revoke runner %s: %w", id, err)
	}
	if err := requireRow(res); err != nil {
		return fmt.Errorf("revoke runner %s: %w", id, err)
	}
	return nil
}

func (r *Runners) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM runners WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete runner %s: %w", id, err)
	}
	if err := requireRow(res); err != nil {
		return fmt.Errorf("delete runner %s: %w", id, err)
	}
	return nil
}

func encodeCapabilities(capabilities []string) (string, error) {
	if capabilities == nil {
		capabilities = []string{}
	}
	data, err := json.Marshal(capabilities)
	if err != nil {
		return "", fmt.Errorf("encode capabilities: %w", err)
	}
	return string(data), nil
}

func scanRunner(scan func(dest ...any) error) (Runner, error) {
	var (
		runner       Runner
		capabilities string
		createdAt    string
		lastSeenAt   sql.NullString
		revokedAt    sql.NullString
		version      sql.NullString
	)
	if err := scan(
		&runner.ID,
		&runner.Name,
		&runner.TokenHash,
		&capabilities,
		&runner.Capacity,
		&createdAt,
		&lastSeenAt,
		&revokedAt,
		&version,
	); err != nil {
		return Runner{}, err
	}
	if err := json.Unmarshal([]byte(capabilities), &runner.Capabilities); err != nil {
		return Runner{}, fmt.Errorf("decode capabilities %q: %w", capabilities, err)
	}
	runner.Version = version.String

	var err error
	if runner.CreatedAt, err = parseTime(createdAt); err != nil {
		return Runner{}, err
	}
	if runner.LastSeenAt, err = scanNullTime(lastSeenAt); err != nil {
		return Runner{}, err
	}
	if runner.RevokedAt, err = scanNullTime(revokedAt); err != nil {
		return Runner{}, err
	}
	return runner, nil
}
