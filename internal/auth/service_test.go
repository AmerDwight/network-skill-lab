package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AmerDwight/network-skill-lab/internal/store"
)

const testPassword = "secret123"

func newService(t *testing.T) (*Service, *store.Store) {
	t.Helper()

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return New(st), st
}

func newUser(t *testing.T, st *store.Store, username string, disabled bool) store.User {
	t.Helper()

	hash, err := HashPassword(testPassword)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	user := store.User{ID: store.NewID(), Username: username, PasswordHash: hash, Role: store.RoleUser}
	if err := st.Users.Create(t.Context(), user); err != nil {
		t.Fatalf("Users.Create() error = %v", err)
	}
	if disabled {
		if err := st.Users.SetDisabled(t.Context(), user.ID, true); err != nil {
			t.Fatalf("SetDisabled() error = %v", err)
		}
	}
	return user
}

func TestLoginCreatesASession(t *testing.T) {
	svc, st := newService(t)
	ctx := t.Context()
	user := newUser(t, st, "alice", false)

	session, loggedIn, err := svc.Login(ctx, "alice", testPassword, "curl/8")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if loggedIn.ID != user.ID {
		t.Fatalf("Login() user = %+v", loggedIn)
	}
	if session.UserID != user.ID || session.UserAgent != "curl/8" {
		t.Fatalf("Login() session = %+v", session)
	}
	if len(session.ID) < 40 {
		t.Errorf("session id = %q, want 32 random bytes", session.ID)
	}
	if got := session.ExpiresAt.Sub(session.CreatedAt); got != SessionTTL {
		t.Errorf("session lifetime = %v, want %v", got, SessionTTL)
	}

	stored, err := st.Sessions.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Sessions.Get() error = %v", err)
	}
	if stored.UserID != user.ID {
		t.Errorf("stored session = %+v", stored)
	}
}

func TestLoginRejectsBadCredentials(t *testing.T) {
	svc, st := newService(t)
	ctx := t.Context()
	newUser(t, st, "alice", false)

	if _, _, err := svc.Login(ctx, "alice", "wrong-password", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("Login(wrong password) error = %v, want ErrInvalidCredentials", err)
	}
	if _, _, err := svc.Login(ctx, "nobody", testPassword, ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("Login(unknown user) error = %v, want ErrInvalidCredentials", err)
	}
	if _, _, err := svc.Login(ctx, store.LocalUsername, testPassword, ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("Login(user without a password) error = %v, want ErrInvalidCredentials", err)
	}
}

func TestLoginRejectsADisabledUser(t *testing.T) {
	svc, st := newService(t)
	newUser(t, st, "alice", true)

	if _, _, err := svc.Login(t.Context(), "alice", testPassword, ""); !errors.Is(err, ErrUserDisabled) {
		t.Fatalf("Login(disabled) error = %v, want ErrUserDisabled", err)
	}
}

func TestLoginBlocksAfterRepeatedFailures(t *testing.T) {
	svc, st := newService(t)
	newUser(t, st, "alice", false)
	svc.limiter.max = 2
	ctx := WithClientKey(t.Context(), "10.0.0.1")

	for range 2 {
		if _, _, err := svc.Login(ctx, "alice", "wrong-password", ""); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("Login() error = %v, want ErrInvalidCredentials", err)
		}
	}
	if _, _, err := svc.Login(ctx, "alice", testPassword, ""); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("Login() after the limit = %v, want ErrTooManyAttempts", err)
	}
	if _, _, err := svc.Login(WithClientKey(t.Context(), "10.0.0.2"), "alice", testPassword, ""); err != nil {
		t.Fatalf("Login() from another client = %v", err)
	}
}

func TestLoginResetsTheLimiterOnSuccess(t *testing.T) {
	svc, st := newService(t)
	newUser(t, st, "alice", false)
	svc.limiter.max = 2
	ctx := WithClientKey(t.Context(), "10.0.0.1")

	if _, _, err := svc.Login(ctx, "alice", "wrong-password", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() error = %v", err)
	}
	if _, _, err := svc.Login(ctx, "alice", testPassword, ""); err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if _, _, err := svc.Login(ctx, "alice", "wrong-password", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() error = %v, want ErrInvalidCredentials", err)
	}
	if _, _, err := svc.Login(ctx, "alice", testPassword, ""); err != nil {
		t.Fatalf("Login() error = %v, the failure count was not reset", err)
	}
}

func TestAuthenticateReturnsTheUser(t *testing.T) {
	svc, st := newService(t)
	ctx := t.Context()
	user := newUser(t, st, "alice", false)

	session, _, err := svc.Login(ctx, "alice", testPassword, "")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	got, err := svc.Authenticate(ctx, session.ID)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if got.ID != user.ID {
		t.Fatalf("Authenticate() = %+v", got)
	}

	if _, err := svc.Authenticate(ctx, "no-such-session"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Authenticate(unknown) error = %v, want ErrNotFound", err)
	}
}

func TestAuthenticateRejectsAnExpiredSession(t *testing.T) {
	svc, st := newService(t)
	ctx := t.Context()
	user := newUser(t, st, "alice", false)

	now := time.Now()
	session := store.Session{
		ID:         "expired-session",
		UserID:     user.ID,
		CreatedAt:  now.Add(-8 * 24 * time.Hour),
		LastSeenAt: now.Add(-8 * 24 * time.Hour),
		ExpiresAt:  now.Add(-time.Minute),
	}
	if err := st.Sessions.Create(ctx, session); err != nil {
		t.Fatalf("Sessions.Create() error = %v", err)
	}

	if _, err := svc.Authenticate(ctx, session.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Authenticate(expired) error = %v, want ErrNotFound", err)
	}
}

func TestAuthenticateRejectsADisabledUser(t *testing.T) {
	svc, st := newService(t)
	ctx := t.Context()
	user := newUser(t, st, "alice", false)

	session, _, err := svc.Login(ctx, "alice", testPassword, "")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if err := st.Users.SetDisabled(ctx, user.ID, true); err != nil {
		t.Fatalf("SetDisabled() error = %v", err)
	}

	if _, err := svc.Authenticate(ctx, session.ID); !errors.Is(err, ErrUserDisabled) {
		t.Fatalf("Authenticate(disabled) error = %v, want ErrUserDisabled", err)
	}
}

func TestAuthenticateTouchesTheSessionAtMostEveryFiveMinutes(t *testing.T) {
	svc, st := newService(t)
	ctx := t.Context()
	newUser(t, st, "alice", false)

	start := time.Now()
	svc.now = func() time.Time { return start }
	session, _, err := svc.Login(ctx, "alice", testPassword, "")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	svc.now = func() time.Time { return start.Add(TouchInterval - time.Second) }
	if _, err := svc.Authenticate(ctx, session.ID); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	stored, err := st.Sessions.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Sessions.Get() error = %v", err)
	}
	if !stored.LastSeenAt.Equal(session.LastSeenAt.UTC().Truncate(time.Millisecond)) {
		t.Fatalf("last_seen_at = %v, want it untouched at %v", stored.LastSeenAt, session.LastSeenAt)
	}

	later := start.Add(TouchInterval + time.Second)
	svc.now = func() time.Time { return later }
	if _, err := svc.Authenticate(ctx, session.ID); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	stored, err = st.Sessions.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Sessions.Get() error = %v", err)
	}
	if !stored.LastSeenAt.Equal(later.UTC().Truncate(time.Millisecond)) {
		t.Errorf("last_seen_at = %v, want %v", stored.LastSeenAt, later.UTC())
	}
	if !stored.ExpiresAt.Equal(later.Add(SessionTTL).UTC().Truncate(time.Millisecond)) {
		t.Errorf("expires_at = %v, want the sliding expiry %v", stored.ExpiresAt, later.Add(SessionTTL).UTC())
	}
}

func TestLogoutDeletesTheSession(t *testing.T) {
	svc, st := newService(t)
	ctx := t.Context()
	newUser(t, st, "alice", false)

	session, _, err := svc.Login(ctx, "alice", testPassword, "")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if err := svc.Logout(ctx, session.ID); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if _, err := svc.Authenticate(ctx, session.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Authenticate() after logout = %v, want ErrNotFound", err)
	}
	if err := svc.Logout(ctx, session.ID); err != nil {
		t.Errorf("Logout() twice error = %v", err)
	}
}

func TestNewSessionIDIsRandom(t *testing.T) {
	seen := make(map[string]bool)
	for range 100 {
		id, err := NewSessionID()
		if err != nil {
			t.Fatalf("NewSessionID() error = %v", err)
		}
		if seen[id] {
			t.Fatalf("NewSessionID() returned %q twice", id)
		}
		seen[id] = true
	}
}

func TestRateLimiterBlocksAndForgets(t *testing.T) {
	now := time.Now()
	limiter := NewRateLimiter()
	limiter.now = func() time.Time { return now }

	for range MaxLoginFailures - 1 {
		limiter.Fail("10.0.0.1")
	}
	if err := limiter.Allow("10.0.0.1"); err != nil {
		t.Fatalf("Allow() before the limit = %v", err)
	}

	limiter.Fail("10.0.0.1")
	if err := limiter.Allow("10.0.0.1"); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("Allow() at the limit = %v, want ErrTooManyAttempts", err)
	}
	if err := limiter.Allow("10.0.0.2"); err != nil {
		t.Errorf("Allow() for another key = %v", err)
	}
	if err := limiter.Allow(""); err != nil {
		t.Errorf("Allow(empty key) = %v", err)
	}

	now = now.Add(LoginBlockFor + time.Second)
	if err := limiter.Allow("10.0.0.1"); err != nil {
		t.Fatalf("Allow() after the block expired = %v", err)
	}
	if len(limiter.entries) != 0 {
		t.Errorf("limiter kept %d entries after the block expired", len(limiter.entries))
	}
}

func TestRateLimiterResetClearsTheFailures(t *testing.T) {
	limiter := NewRateLimiter()
	for range MaxLoginFailures - 1 {
		limiter.Fail("10.0.0.1")
	}
	limiter.Reset("10.0.0.1")
	limiter.Fail("10.0.0.1")

	if err := limiter.Allow("10.0.0.1"); err != nil {
		t.Fatalf("Allow() after Reset = %v", err)
	}
}

func TestClientKeyRoundTrip(t *testing.T) {
	if got := ClientKey(context.Background()); got != "" {
		t.Errorf("ClientKey() = %q, want empty", got)
	}
	if got := ClientKey(WithClientKey(context.Background(), "10.0.0.1")); got != "10.0.0.1" {
		t.Errorf("ClientKey() = %q", got)
	}
}
