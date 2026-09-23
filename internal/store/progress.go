package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const (
	ProgressDoc = "doc"
	ProgressLab = "lab"
)

type Progress struct {
	db *sql.DB
}

func (p *Progress) Upsert(ctx context.Context, userID, kind, ref string) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO progress (user_id, kind, ref, completed_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id, kind, ref) DO UPDATE SET completed_at = excluded.completed_at`,
		userID, kind, ref, formatTime(time.Now()))
	if err != nil {
		return fmt.Errorf("record progress %s/%s for user %s: %w", kind, ref, userID, err)
	}
	return nil
}

func (p *Progress) ListByUser(ctx context.Context, userID string) ([]ProgressEntry, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT user_id, kind, ref, completed_at FROM progress WHERE user_id = ? ORDER BY kind, ref`, userID)
	if err != nil {
		return nil, fmt.Errorf("list progress of user %s: %w", userID, err)
	}
	defer func() { _ = rows.Close() }()

	var entries []ProgressEntry
	for rows.Next() {
		var (
			entry       ProgressEntry
			completedAt string
		)
		if err := rows.Scan(&entry.UserID, &entry.Kind, &entry.Ref, &completedAt); err != nil {
			return nil, fmt.Errorf("list progress of user %s: %w", userID, err)
		}
		if entry.CompletedAt, err = parseTime(completedAt); err != nil {
			return nil, fmt.Errorf("list progress of user %s: %w", userID, err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list progress of user %s: %w", userID, err)
	}
	return entries, nil
}
