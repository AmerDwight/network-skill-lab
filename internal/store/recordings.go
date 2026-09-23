package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Recordings struct {
	db *sql.DB
}

const recordingColumns = `id, attempt_id, node, tab_id, path, started_at, ended_at`

func (r *Recordings) Create(ctx context.Context, rec Recording) error {
	if rec.StartedAt.IsZero() {
		rec.StartedAt = time.Now()
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO recordings (`+recordingColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.AttemptID, rec.Node, rec.TabID, rec.Path, formatTime(rec.StartedAt), nullTime(rec.EndedAt))
	if err != nil {
		return fmt.Errorf("create recording %s: %w", rec.ID, err)
	}
	return nil
}

func (r *Recordings) SetEnded(ctx context.Context, id string, t time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE recordings SET ended_at = ? WHERE id = ?`, formatTime(t), id)
	if err != nil {
		return fmt.Errorf("set ended_at of recording %s: %w", id, err)
	}
	if err := requireRow(res); err != nil {
		return fmt.Errorf("set ended_at of recording %s: %w", id, err)
	}
	return nil
}

func (r *Recordings) ListByAttempt(ctx context.Context, attemptID string) ([]Recording, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+recordingColumns+` FROM recordings WHERE attempt_id = ? ORDER BY started_at, id`,
		attemptID)
	if err != nil {
		return nil, fmt.Errorf("list recordings of attempt %s: %w", attemptID, err)
	}
	defer func() { _ = rows.Close() }()

	var recordings []Recording
	for rows.Next() {
		var (
			rec       Recording
			startedAt string
			endedAt   sql.NullString
		)
		if err := rows.Scan(&rec.ID, &rec.AttemptID, &rec.Node, &rec.TabID, &rec.Path, &startedAt, &endedAt); err != nil {
			return nil, fmt.Errorf("list recordings of attempt %s: %w", attemptID, err)
		}
		if rec.StartedAt, err = parseTime(startedAt); err != nil {
			return nil, fmt.Errorf("list recordings of attempt %s: %w", attemptID, err)
		}
		if rec.EndedAt, err = scanNullTime(endedAt); err != nil {
			return nil, fmt.Errorf("list recordings of attempt %s: %w", attemptID, err)
		}
		recordings = append(recordings, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list recordings of attempt %s: %w", attemptID, err)
	}
	return recordings, nil
}
