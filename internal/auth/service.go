package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/store"
)

const (
	SessionTTL    = 7 * 24 * time.Hour
	TouchInterval = 5 * time.Minute

	sessionIDLen = 32
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserDisabled       = errors.New("user is disabled")
)

type clientKeyContextKey struct{}

func WithClientKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, clientKeyContextKey{}, key)
}

func ClientKey(ctx context.Context) string {
	key, _ := ctx.Value(clientKeyContextKey{}).(string)
	return key
}

type Service struct {
	store   *store.Store
	limiter *RateLimiter
	now     func() time.Time
}

func New(st *store.Store) *Service {
	return &Service{store: st, limiter: NewRateLimiter(), now: time.Now}
}

func NewSessionID() (string, error) {
	buf := make([]byte, sessionIDLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (s *Service) Login(ctx context.Context, username, password, userAgent string) (store.Session, store.User, error) {
	key := ClientKey(ctx)
	if err := s.limiter.Allow(key); err != nil {
		return store.Session{}, store.User{}, err
	}

	user, err := s.store.Users.ByUsername(ctx, username)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return store.Session{}, store.User{}, err
	}

	hash := user.PasswordHash
	if hash == "" {
		hash = placeholderHash()
	}
	match, verifyErr := VerifyPassword(hash, password)
	if verifyErr != nil {
		return store.Session{}, store.User{}, verifyErr
	}
	if err != nil || user.PasswordHash == "" || !match {
		s.limiter.Fail(key)
		return store.Session{}, store.User{}, ErrInvalidCredentials
	}
	if user.Disabled() {
		return store.Session{}, store.User{}, ErrUserDisabled
	}
	s.limiter.Reset(key)

	id, err := NewSessionID()
	if err != nil {
		return store.Session{}, store.User{}, err
	}
	now := s.now()
	session := store.Session{
		ID:         id,
		UserID:     user.ID,
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  now.Add(SessionTTL),
		UserAgent:  userAgent,
	}
	if err := s.store.Sessions.Create(ctx, session); err != nil {
		return store.Session{}, store.User{}, err
	}
	return session, user, nil
}

func (s *Service) Authenticate(ctx context.Context, sessionID string) (store.User, error) {
	session, err := s.store.Sessions.Get(ctx, sessionID)
	if err != nil {
		return store.User{}, err
	}

	user, err := s.store.Users.ByID(ctx, session.UserID)
	if err != nil {
		return store.User{}, err
	}
	if user.Disabled() {
		return store.User{}, ErrUserDisabled
	}

	now := s.now()
	if now.Sub(session.LastSeenAt) >= TouchInterval {
		if err := s.store.Sessions.Touch(ctx, session.ID, now, now.Add(SessionTTL)); err != nil {
			return store.User{}, err
		}
	}
	return user, nil
}

func (s *Service) Logout(ctx context.Context, sessionID string) error {
	if err := s.store.Sessions.Delete(ctx, sessionID); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return nil
}

var placeholderHash = sync.OnceValue(func() string {
	hash, err := HashPassword("placeholder")
	if err != nil {
		panic(err)
	}
	return hash
})
