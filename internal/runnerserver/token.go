package runnerserver

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"

	"github.com/AmerDwight/network-skill-lab/internal/auth"
	"github.com/AmerDwight/network-skill-lab/internal/runner"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

const tokenBytes = 32

var (
	ErrInvalidName = errors.New("invalid runner name")

	namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,31}$`)
)

func ValidateName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("%w %q: 3 to 32 characters of a-z, 0-9, _ or -, starting with a letter or digit", ErrInvalidName, name)
	}
	return nil
}

func NewToken() (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate runner token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Register creates a runner row and returns the plaintext token, which is not stored
// anywhere and cannot be recovered later.
func Register(ctx context.Context, runners *store.Runners, name string, capabilities []string, capacity int) (store.Runner, string, error) {
	if err := ValidateName(name); err != nil {
		return store.Runner{}, "", err
	}
	if len(capabilities) == 0 {
		capabilities = []string{string(runner.EnvContainer)}
	}
	token, err := NewToken()
	if err != nil {
		return store.Runner{}, "", err
	}
	hash, err := auth.HashPassword(token)
	if err != nil {
		return store.Runner{}, "", fmt.Errorf("hash runner token: %w", err)
	}
	record := store.Runner{ID: store.NewID(), Name: name, TokenHash: hash, Capabilities: capabilities, Capacity: capacity}
	if err := runners.Create(ctx, record); err != nil {
		return store.Runner{}, "", err
	}
	return record, token, nil
}
