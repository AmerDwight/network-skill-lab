package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type Users struct {
	db *sql.DB
}

const userColumns = `id, username, password_hash, role, locale, created_at`

func (u *Users) Local(ctx context.Context) (User, error) {
	row := u.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE username = ?`, LocalUsername)

	user, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, fmt.Errorf("local user: %w", ErrNotFound)
	}
	if err != nil {
		return User{}, fmt.Errorf("local user: %w", err)
	}
	return user, nil
}

func scanUser(row *sql.Row) (User, error) {
	var (
		user         User
		passwordHash sql.NullString
		createdAt    string
	)
	if err := row.Scan(&user.ID, &user.Username, &passwordHash, &user.Role, &user.Locale, &createdAt); err != nil {
		return User{}, err
	}
	user.PasswordHash = passwordHash.String

	var err error
	if user.CreatedAt, err = parseTime(createdAt); err != nil {
		return User{}, err
	}
	return user, nil
}
