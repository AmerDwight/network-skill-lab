package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Sessions struct {
	db *sql.DB
}

const sessionColumns = `id, user_id, created_at, last_seen_at, expires_at, user_agent`

func (s *Sessions) Create(ctx context.Context, session Session) error {
	if session.CreatedAt.IsZero() {
		session.CreatedAt = time.Now()
	}
	if session.LastSeenAt.IsZero() {
		session.LastSeenAt = session.CreatedAt
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (`+sessionColumns+`) VALUES (?, ?, ?, ?, ?, ?)`,
		session.ID,
		session.UserID,
		formatTime(session.CreatedAt),
		formatTime(session.LastSeenAt),
		formatTime(session.ExpiresAt),
		nullString(session.UserAgent),
	)
	if err != nil {
		return fmt.Errorf("create session for user %s: %w", session.UserID, err)
	}
	return nil
}

func (s *Sessions) Get(ctx context.Context, id string) (Session, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+sessionColumns+` FROM sessions WHERE id = ? AND expires_at > ?`,
		id, formatTime(time.Now()))

	session, err := scanSession(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, fmt.Errorf("get session: %w", ErrNotFound)
	}
	if err != nil {
		return Session{}, fmt.Errorf("get session: %w", err)
	}
	return session, nil
}

func (s *Sessions) Touch(ctx context.Context, id string, lastSeen, expires time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id = ?`,
		formatTime(lastSeen), formatTime(expires), id)
	if err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	if err := requireRow(res); err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

func (s *Sessions) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	if err := requireRow(res); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s *Sessions) DeleteByUser(ctx context.Context, userID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("delete sessions of user %s: %w", userID, err)
	}
	return nil
}

func (s *Sessions) DeleteExpired(ctx context.Context, now time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, formatTime(now))
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return int(n), nil
}

func scanSession(scan func(dest ...any) error) (Session, error) {
	var (
		session    Session
		createdAt  string
		lastSeenAt string
		expiresAt  string
		userAgent  sql.NullString
	)
	if err := scan(&session.ID, &session.UserID, &createdAt, &lastSeenAt, &expiresAt, &userAgent); err != nil {
		return Session{}, err
	}
	session.UserAgent = userAgent.String

	var err error
	if session.CreatedAt, err = parseTime(createdAt); err != nil {
		return Session{}, err
	}
	if session.LastSeenAt, err = parseTime(lastSeenAt); err != nil {
		return Session{}, err
	}
	if session.ExpiresAt, err = parseTime(expiresAt); err != nil {
		return Session{}, err
	}
	return session, nil
}
