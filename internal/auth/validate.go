package auth

import (
	"errors"
	"fmt"
	"regexp"
)

const MinPasswordLength = 8

var (
	ErrInvalidUsername = errors.New("invalid username")
	ErrInvalidPassword = errors.New("invalid password")

	usernamePattern = regexp.MustCompile(`^[a-z0-9_-]{3,32}$`)
)

func ValidateUsername(username string) error {
	if !usernamePattern.MatchString(username) {
		return fmt.Errorf("%w: 3 to 32 characters of a-z, 0-9, _ or -", ErrInvalidUsername)
	}
	return nil
}

func ValidatePassword(password string) error {
	if len(password) < MinPasswordLength {
		return fmt.Errorf("%w: at least %d characters", ErrInvalidPassword, MinPasswordLength)
	}
	return nil
}
