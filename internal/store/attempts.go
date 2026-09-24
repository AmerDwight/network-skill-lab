package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Attempts struct {
	db *sql.DB
}

const attemptColumns = `id, user_id, lab_id, lab_version, case_id, mode, params_json, seed, submit_count, status,
	error_message, started_at, ended_at, elapsed_ms, runner_id, sandbox_id, created_at`

func (a *Attempts) Create(ctx context.Context, attempt Attempt) error {
	if attempt.CreatedAt.IsZero() {
		attempt.CreatedAt = time.Now()
	}
	_, err := a.db.ExecContext(ctx,
		`INSERT INTO attempts (`+attemptColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		attempt.ID,
		attempt.UserID,
		attempt.LabID,
		attempt.LabVersion,
		nullString(attempt.CaseID),
		attempt.Mode,
		attempt.ParamsJSON,
		nullInt64(attempt.Seed),
		attempt.SubmitCount,
		attempt.Status,
		nullString(attempt.ErrorMessage),
		nullTime(attempt.StartedAt),
		nullTime(attempt.EndedAt),
		attempt.ElapsedMS,
		nullString(attempt.RunnerID),
		nullString(attempt.SandboxID),
		formatTime(attempt.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("create attempt %s: %w", attempt.ID, err)
	}
	return nil
}

func (a *Attempts) Get(ctx context.Context, id string) (Attempt, error) {
	row := a.db.QueryRowContext(ctx, `SELECT `+attemptColumns+` FROM attempts WHERE id = ?`, id)

	attempt, err := scanAttempt(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Attempt{}, fmt.Errorf("get attempt %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Attempt{}, fmt.Errorf("get attempt %s: %w", id, err)
	}
	return attempt, nil
}

func (a *Attempts) ActiveForUser(ctx context.Context, userID string) (Attempt, bool, error) {
	row := a.db.QueryRowContext(ctx,
		`SELECT `+attemptColumns+` FROM attempts
		WHERE user_id = ? AND status IN (?, ?)
		ORDER BY created_at DESC, id DESC LIMIT 1`,
		userID, StatusProvisioning, StatusRunning)

	attempt, err := scanAttempt(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Attempt{}, false, nil
	}
	if err != nil {
		return Attempt{}, false, fmt.Errorf("active attempt for user %s: %w", userID, err)
	}
	return attempt, true, nil
}

func (a *Attempts) ListByUser(ctx context.Context, userID string, before time.Time, limit int) ([]Attempt, error) {
	query := `SELECT ` + attemptColumns + ` FROM attempts WHERE user_id = ?`
	args := []any{userID}
	if !before.IsZero() {
		query += ` AND created_at < ?`
		args = append(args, formatTime(before))
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list attempts of user %s: %w", userID, err)
	}
	defer func() { _ = rows.Close() }()

	var attempts []Attempt
	for rows.Next() {
		attempt, err := scanAttempt(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("list attempts of user %s: %w", userID, err)
		}
		attempts = append(attempts, attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list attempts of user %s: %w", userID, err)
	}
	return attempts, nil
}

func (a *Attempts) CountActive(ctx context.Context) (int, error) {
	var count int
	if err := a.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM attempts WHERE status IN (?, ?)`,
		StatusProvisioning, StatusRunning).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active attempts: %w", err)
	}
	return count, nil
}

func (a *Attempts) ListNonTerminal(ctx context.Context) ([]Attempt, error) {
	rows, err := a.db.QueryContext(ctx,
		`SELECT `+attemptColumns+` FROM attempts
		WHERE status IN (?, ?)
		ORDER BY created_at, id`,
		StatusProvisioning, StatusRunning)
	if err != nil {
		return nil, fmt.Errorf("list non-terminal attempts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var attempts []Attempt
	for rows.Next() {
		attempt, err := scanAttempt(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("list non-terminal attempts: %w", err)
		}
		attempts = append(attempts, attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list non-terminal attempts: %w", err)
	}
	return attempts, nil
}

func (a *Attempts) SetStatus(ctx context.Context, id, status string, endedAt *time.Time, errorMessage string) error {
	res, err := a.db.ExecContext(ctx,
		`UPDATE attempts SET status = ?, ended_at = ?, error_message = ? WHERE id = ?`,
		status, nullTime(endedAt), nullString(errorMessage), id)
	if err != nil {
		return fmt.Errorf("set status of attempt %s: %w", id, err)
	}
	if err := requireRow(res); err != nil {
		return fmt.Errorf("set status of attempt %s: %w", id, err)
	}
	return nil
}

func (a *Attempts) SetSandbox(ctx context.Context, id, runnerID, sandboxID string) error {
	res, err := a.db.ExecContext(ctx,
		`UPDATE attempts SET runner_id = ?, sandbox_id = ? WHERE id = ?`,
		nullString(runnerID), nullString(sandboxID), id)
	if err != nil {
		return fmt.Errorf("set sandbox of attempt %s: %w", id, err)
	}
	if err := requireRow(res); err != nil {
		return fmt.Errorf("set sandbox of attempt %s: %w", id, err)
	}
	return nil
}

func (a *Attempts) SetElapsed(ctx context.Context, id string, elapsed time.Duration) error {
	res, err := a.db.ExecContext(ctx,
		`UPDATE attempts SET elapsed_ms = ? WHERE id = ?`, elapsed.Milliseconds(), id)
	if err != nil {
		return fmt.Errorf("set elapsed of attempt %s: %w", id, err)
	}
	if err := requireRow(res); err != nil {
		return fmt.Errorf("set elapsed of attempt %s: %w", id, err)
	}
	return nil
}

func (a *Attempts) SetStartedAt(ctx context.Context, id string, t time.Time) error {
	res, err := a.db.ExecContext(ctx,
		`UPDATE attempts SET started_at = ? WHERE id = ?`, formatTime(t), id)
	if err != nil {
		return fmt.Errorf("set started_at of attempt %s: %w", id, err)
	}
	if err := requireRow(res); err != nil {
		return fmt.Errorf("set started_at of attempt %s: %w", id, err)
	}
	return nil
}

func (a *Attempts) SetResolved(ctx context.Context, id string, seed int64, caseID, paramsJSON string) error {
	res, err := a.db.ExecContext(ctx,
		`UPDATE attempts SET seed = ?, case_id = ?, params_json = ? WHERE id = ?`,
		seed, nullString(caseID), paramsJSON, id)
	if err != nil {
		return fmt.Errorf("set resolved params of attempt %s: %w", id, err)
	}
	if err := requireRow(res); err != nil {
		return fmt.Errorf("set resolved params of attempt %s: %w", id, err)
	}
	return nil
}

func (a *Attempts) IncrementSubmitCount(ctx context.Context, id string) error {
	res, err := a.db.ExecContext(ctx, `UPDATE attempts SET submit_count = submit_count + 1 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("increment submit count of attempt %s: %w", id, err)
	}
	if err := requireRow(res); err != nil {
		return fmt.Errorf("increment submit count of attempt %s: %w", id, err)
	}
	return nil
}

func scanAttempt(scan func(dest ...any) error) (Attempt, error) {
	var (
		attempt      Attempt
		caseID       sql.NullString
		seed         sql.NullInt64
		errorMessage sql.NullString
		startedAt    sql.NullString
		endedAt      sql.NullString
		runnerID     sql.NullString
		sandboxID    sql.NullString
		createdAt    string
	)
	if err := scan(
		&attempt.ID,
		&attempt.UserID,
		&attempt.LabID,
		&attempt.LabVersion,
		&caseID,
		&attempt.Mode,
		&attempt.ParamsJSON,
		&seed,
		&attempt.SubmitCount,
		&attempt.Status,
		&errorMessage,
		&startedAt,
		&endedAt,
		&attempt.ElapsedMS,
		&runnerID,
		&sandboxID,
		&createdAt,
	); err != nil {
		return Attempt{}, err
	}

	attempt.CaseID = caseID.String
	if seed.Valid {
		attempt.Seed = &seed.Int64
	}
	attempt.ErrorMessage = errorMessage.String
	attempt.RunnerID = runnerID.String
	attempt.SandboxID = sandboxID.String

	var err error
	if attempt.StartedAt, err = scanNullTime(startedAt); err != nil {
		return Attempt{}, err
	}
	if attempt.EndedAt, err = scanNullTime(endedAt); err != nil {
		return Attempt{}, err
	}
	if attempt.CreatedAt, err = parseTime(createdAt); err != nil {
		return Attempt{}, err
	}
	return attempt, nil
}
