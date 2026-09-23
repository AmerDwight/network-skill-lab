package store

import (
	"context"
	"database/sql"
	"fmt"
)

type CommandLog struct {
	db *sql.DB
}

func (c *CommandLog) AppendBatch(ctx context.Context, entries []CommandEntry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("append command log: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO command_log (attempt_id, node, ts, user, cwd, command, exit_code)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("append command log: prepare: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, e := range entries {
		if _, err := stmt.ExecContext(ctx,
			e.AttemptID, e.Node, formatTime(e.TS), nullString(e.User), nullString(e.CWD), e.Command, e.ExitCode,
		); err != nil {
			return fmt.Errorf("append command log for attempt %s: %w", e.AttemptID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("append command log: commit: %w", err)
	}
	return nil
}

func (c *CommandLog) CountByAttempt(ctx context.Context, attemptID string) (int, error) {
	var count int
	if err := c.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM command_log WHERE attempt_id = ?`, attemptID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count command log of attempt %s: %w", attemptID, err)
	}
	return count, nil
}

func (c *CommandLog) ListByAttempt(ctx context.Context, attemptID string) ([]CommandEntry, error) {
	rows, err := c.db.QueryContext(ctx,
		`SELECT id, attempt_id, node, ts, user, cwd, command, exit_code
		FROM command_log WHERE attempt_id = ? ORDER BY ts, id`, attemptID)
	if err != nil {
		return nil, fmt.Errorf("list command log of attempt %s: %w", attemptID, err)
	}
	defer func() { _ = rows.Close() }()

	var entries []CommandEntry
	for rows.Next() {
		var (
			entry    CommandEntry
			ts       string
			user     sql.NullString
			cwd      sql.NullString
			exitCode sql.NullInt64
		)
		if err := rows.Scan(&entry.ID, &entry.AttemptID, &entry.Node, &ts, &user, &cwd, &entry.Command, &exitCode); err != nil {
			return nil, fmt.Errorf("list command log of attempt %s: %w", attemptID, err)
		}
		if entry.TS, err = parseTime(ts); err != nil {
			return nil, fmt.Errorf("list command log of attempt %s: %w", attemptID, err)
		}
		entry.User = user.String
		entry.CWD = cwd.String
		entry.ExitCode = int(exitCode.Int64)
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list command log of attempt %s: %w", attemptID, err)
	}
	return entries, nil
}
