package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	sqlite "modernc.org/sqlite"
)

var ErrUsernameTaken = errors.New("username already taken")

const sqliteConstraintUnique = 2067

type Users struct {
	db *sql.DB
}

const userColumns = `id, username, password_hash, role, locale, created_at, disabled_at, updated_at`

func (u *Users) Create(ctx context.Context, user User) error {
	if user.CreatedAt.IsZero() {
		user.CreatedAt = time.Now()
	}
	if user.Role == "" {
		user.Role = RoleUser
	}
	if user.Locale == "" {
		user.Locale = DefaultLocale
	}
	if user.UpdatedAt == nil {
		user.UpdatedAt = &user.CreatedAt
	}

	_, err := u.db.ExecContext(ctx,
		`INSERT INTO users (`+userColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID,
		user.Username,
		nullString(user.PasswordHash),
		user.Role,
		user.Locale,
		formatTime(user.CreatedAt),
		nullTime(user.DisabledAt),
		nullTime(user.UpdatedAt),
	)
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) && sqliteErr.Code() == sqliteConstraintUnique {
		return fmt.Errorf("create user %s: %w", user.Username, ErrUsernameTaken)
	}
	if err != nil {
		return fmt.Errorf("create user %s: %w", user.Username, err)
	}
	return nil
}

func (u *Users) ByUsername(ctx context.Context, username string) (User, error) {
	row := u.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE username = ?`, username)

	user, err := scanUser(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, fmt.Errorf("user %s: %w", username, ErrNotFound)
	}
	if err != nil {
		return User{}, fmt.Errorf("user %s: %w", username, err)
	}
	return user, nil
}

func (u *Users) ByID(ctx context.Context, id string) (User, error) {
	row := u.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id)

	user, err := scanUser(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, fmt.Errorf("user %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return User{}, fmt.Errorf("user %s: %w", id, err)
	}
	return user, nil
}

func (u *Users) Local(ctx context.Context) (User, error) {
	return u.ByUsername(ctx, LocalUsername)
}

const userSummaryColumns = userColumns + `,
	(SELECT COUNT(*) FROM attempts WHERE attempts.user_id = users.id)`

func (u *Users) SummaryByID(ctx context.Context, id string) (UserSummary, error) {
	row := u.db.QueryRowContext(ctx, `SELECT `+userSummaryColumns+` FROM users WHERE id = ?`, id)

	var summary UserSummary
	user, err := scanUser(func(dest ...any) error {
		return row.Scan(append(dest, &summary.Attempts)...)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return UserSummary{}, fmt.Errorf("user %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return UserSummary{}, fmt.Errorf("user %s: %w", id, err)
	}
	summary.User = user
	return summary, nil
}

func (u *Users) List(ctx context.Context) ([]UserSummary, error) {
	rows, err := u.db.QueryContext(ctx,
		`SELECT `+userSummaryColumns+` FROM users ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var summaries []UserSummary
	for rows.Next() {
		var summary UserSummary
		summary.User, err = scanUser(func(dest ...any) error {
			return rows.Scan(append(dest, &summary.Attempts)...)
		})
		if err != nil {
			return nil, fmt.Errorf("list users: %w", err)
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return summaries, nil
}

func (u *Users) SetPasswordHash(ctx context.Context, id, hash string) error {
	return u.update(ctx, id, "password_hash", nullString(hash))
}

func (u *Users) SetRole(ctx context.Context, id, role string) error {
	return u.update(ctx, id, "role", role)
}

func (u *Users) SetLocale(ctx context.Context, id, locale string) error {
	return u.update(ctx, id, "locale", locale)
}

func (u *Users) SetDisabled(ctx context.Context, id string, disabled bool) error {
	var disabledAt any
	if disabled {
		disabledAt = formatTime(time.Now())
	}
	return u.update(ctx, id, "disabled_at", disabledAt)
}

func (u *Users) Count(ctx context.Context) (int, error) {
	var count int
	if err := u.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return count, nil
}

func (u *Users) CountEnabled(ctx context.Context) (int, error) {
	var count int
	if err := u.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE disabled_at IS NULL AND password_hash IS NOT NULL`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count enabled users: %w", err)
	}
	return count, nil
}

func (u *Users) update(ctx context.Context, id, column string, value any) error {
	res, err := u.db.ExecContext(ctx,
		`UPDATE users SET `+column+` = ?, updated_at = ? WHERE id = ?`,
		value, formatTime(time.Now()), id)
	if err != nil {
		return fmt.Errorf("set %s of user %s: %w", column, id, err)
	}
	if err := requireRow(res); err != nil {
		return fmt.Errorf("set %s of user %s: %w", column, id, err)
	}
	return nil
}

func scanUser(scan func(dest ...any) error) (User, error) {
	var (
		user         User
		passwordHash sql.NullString
		createdAt    string
		disabledAt   sql.NullString
		updatedAt    sql.NullString
	)
	if err := scan(
		&user.ID,
		&user.Username,
		&passwordHash,
		&user.Role,
		&user.Locale,
		&createdAt,
		&disabledAt,
		&updatedAt,
	); err != nil {
		return User{}, err
	}
	user.PasswordHash = passwordHash.String

	var err error
	if user.CreatedAt, err = parseTime(createdAt); err != nil {
		return User{}, err
	}
	if user.DisabledAt, err = scanNullTime(disabledAt); err != nil {
		return User{}, err
	}
	if user.UpdatedAt, err = scanNullTime(updatedAt); err != nil {
		return User{}, err
	}
	return user, nil
}
