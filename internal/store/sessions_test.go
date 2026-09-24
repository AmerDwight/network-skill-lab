package store

import (
	"errors"
	"testing"
	"time"
)

func newSession(t *testing.T, st *Store, userID string, expires time.Time) Session {
	t.Helper()
	session := Session{ID: NewID(), UserID: userID, ExpiresAt: expires, UserAgent: "test-agent"}
	if err := st.Sessions.Create(t.Context(), session); err != nil {
		t.Fatalf("Sessions.Create() error = %v", err)
	}
	return session
}

func TestSessionRoundTrip(t *testing.T) {
	st := openStore(t)
	ctx := t.Context()
	user := localUser(t, st)

	session := newSession(t, st, user.ID, time.Now().Add(time.Hour))
	stored, err := st.Sessions.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.UserID != user.ID || stored.UserAgent != "test-agent" {
		t.Fatalf("Get() = %+v", stored)
	}
	if stored.CreatedAt.IsZero() || !stored.LastSeenAt.Equal(stored.CreatedAt) {
		t.Errorf("timestamps = %v / %v", stored.CreatedAt, stored.LastSeenAt)
	}

	lastSeen := time.Now().Add(time.Minute)
	expires := lastSeen.Add(2 * time.Hour)
	if err := st.Sessions.Touch(ctx, session.ID, lastSeen, expires); err != nil {
		t.Fatalf("Touch() error = %v", err)
	}
	stored, err = st.Sessions.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !stored.LastSeenAt.Equal(lastSeen.UTC().Truncate(time.Millisecond)) {
		t.Errorf("last_seen_at = %v, want %v", stored.LastSeenAt, lastSeen.UTC())
	}
	if !stored.ExpiresAt.Equal(expires.UTC().Truncate(time.Millisecond)) {
		t.Errorf("expires_at = %v, want %v", stored.ExpiresAt, expires.UTC())
	}

	if err := st.Sessions.Delete(ctx, session.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := st.Sessions.Get(ctx, session.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get() after delete = %v, want ErrNotFound", err)
	}
	if err := st.Sessions.Delete(ctx, session.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete() twice = %v, want ErrNotFound", err)
	}
	if err := st.Sessions.Touch(ctx, session.ID, lastSeen, expires); !errors.Is(err, ErrNotFound) {
		t.Errorf("Touch() after delete = %v, want ErrNotFound", err)
	}
}

func TestSessionGetIgnoresExpiredSessions(t *testing.T) {
	st := openStore(t)
	user := localUser(t, st)

	session := newSession(t, st, user.ID, time.Now().Add(-time.Second))
	if _, err := st.Sessions.Get(t.Context(), session.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() expired = %v, want ErrNotFound", err)
	}
}

func TestSessionDeleteByUserAndExpired(t *testing.T) {
	st := openStore(t)
	ctx := t.Context()
	user := localUser(t, st)

	live := newSession(t, st, user.ID, time.Now().Add(time.Hour))
	expired := newSession(t, st, user.ID, time.Now().Add(-time.Hour))

	deleted, err := st.Sessions.DeleteExpired(ctx, time.Now())
	if err != nil {
		t.Fatalf("DeleteExpired() error = %v", err)
	}
	if deleted != 1 {
		t.Fatalf("DeleteExpired() = %d, want 1", deleted)
	}
	if err := st.Sessions.Delete(ctx, expired.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("the expired session is still stored: %v", err)
	}
	if _, err := st.Sessions.Get(ctx, live.ID); err != nil {
		t.Fatalf("the live session was deleted: %v", err)
	}

	if err := st.Sessions.DeleteByUser(ctx, user.ID); err != nil {
		t.Fatalf("DeleteByUser() error = %v", err)
	}
	if _, err := st.Sessions.Get(ctx, live.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get() after DeleteByUser = %v, want ErrNotFound", err)
	}
	if err := st.Sessions.DeleteByUser(ctx, "nobody"); err != nil {
		t.Errorf("DeleteByUser(unknown) error = %v", err)
	}
}

func TestSessionsCascadeWhenTheUserIsDeleted(t *testing.T) {
	st := openStore(t)
	ctx := t.Context()

	user := User{ID: NewID(), Username: "alice"}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("Users.Create() error = %v", err)
	}
	session := newSession(t, st, user.ID, time.Now().Add(time.Hour))

	if _, err := st.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, user.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if _, err := st.Sessions.Get(ctx, session.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get() after the user was deleted = %v, want ErrNotFound", err)
	}
}
