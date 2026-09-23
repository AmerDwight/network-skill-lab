package store

import (
	"context"
	"database/sql"
	"fmt"
)

type CheckpointRuns struct {
	db *sql.DB
}

const checkpointRunColumns = `attempt_id, checkpoint_id, first_passed_at, last_status, last_run_at`

func (c *CheckpointRuns) Upsert(ctx context.Context, run CheckpointRun) error {
	firstPassedAt := any(nil)
	if run.LastStatus == CheckpointPass {
		if run.FirstPassedAt != nil {
			firstPassedAt = formatTime(*run.FirstPassedAt)
		} else {
			firstPassedAt = nullTime(run.LastRunAt)
		}
	}

	_, err := c.db.ExecContext(ctx,
		`INSERT INTO checkpoint_runs (`+checkpointRunColumns+`)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (attempt_id, checkpoint_id) DO UPDATE SET
			first_passed_at = COALESCE(checkpoint_runs.first_passed_at, excluded.first_passed_at),
			last_status = excluded.last_status,
			last_run_at = excluded.last_run_at`,
		run.AttemptID, run.CheckpointID, firstPassedAt, run.LastStatus, nullTime(run.LastRunAt))
	if err != nil {
		return fmt.Errorf("upsert checkpoint run %s/%s: %w", run.AttemptID, run.CheckpointID, err)
	}
	return nil
}

func (c *CheckpointRuns) ListByAttempt(ctx context.Context, attemptID string) ([]CheckpointRun, error) {
	rows, err := c.db.QueryContext(ctx,
		`SELECT `+checkpointRunColumns+` FROM checkpoint_runs WHERE attempt_id = ? ORDER BY checkpoint_id`,
		attemptID)
	if err != nil {
		return nil, fmt.Errorf("list checkpoint runs of attempt %s: %w", attemptID, err)
	}
	defer func() { _ = rows.Close() }()

	var runs []CheckpointRun
	for rows.Next() {
		var (
			run           CheckpointRun
			firstPassedAt sql.NullString
			lastRunAt     sql.NullString
		)
		if err := rows.Scan(&run.AttemptID, &run.CheckpointID, &firstPassedAt, &run.LastStatus, &lastRunAt); err != nil {
			return nil, fmt.Errorf("list checkpoint runs of attempt %s: %w", attemptID, err)
		}
		if run.FirstPassedAt, err = scanNullTime(firstPassedAt); err != nil {
			return nil, fmt.Errorf("list checkpoint runs of attempt %s: %w", attemptID, err)
		}
		if run.LastRunAt, err = scanNullTime(lastRunAt); err != nil {
			return nil, fmt.Errorf("list checkpoint runs of attempt %s: %w", attemptID, err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list checkpoint runs of attempt %s: %w", attemptID, err)
	}
	return runs, nil
}
