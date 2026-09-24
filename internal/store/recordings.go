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

const recordingColumns = `id, attempt_id, node, tab_id, path, started_at, ended_at, bytes`

func (r *Recordings) Create(ctx context.Context, rec Recording) error {
	if rec.StartedAt.IsZero() {
		rec.StartedAt = time.Now()
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO recordings (`+recordingColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.AttemptID, rec.Node, rec.TabID, rec.Path,
		formatTime(rec.StartedAt), nullTime(rec.EndedAt), nullInt64(rec.Bytes))
	if err != nil {
		return fmt.Errorf("create recording %s: %w", rec.ID, err)
	}
	return nil
}

func (r *Recordings) SetEnded(ctx context.Context, id string, t time.Time, size int64) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE recordings SET ended_at = ?, bytes = ? WHERE id = ?`, formatTime(t), size, id)
	if err != nil {
		return fmt.Errorf("set ended_at of recording %s: %w", id, err)
	}
	if err := requireRow(res); err != nil {
		return fmt.Errorf("set ended_at of recording %s: %w", id, err)
	}
	return nil
}

func (r *Recordings) TotalBytes(ctx context.Context) (int64, error) {
	var total sql.NullInt64
	if err := r.db.QueryRowContext(ctx, `SELECT SUM(bytes) FROM recordings`).Scan(&total); err != nil {
		return 0, fmt.Errorf("total recording bytes: %w", err)
	}
	return total.Int64, nil
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
			size      sql.NullInt64
		)
		if err := rows.Scan(&rec.ID, &rec.AttemptID, &rec.Node, &rec.TabID, &rec.Path, &startedAt, &endedAt, &size); err != nil {
			return nil, fmt.Errorf("list recordings of attempt %s: %w", attemptID, err)
		}
		if size.Valid {
			rec.Bytes = &size.Int64
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
