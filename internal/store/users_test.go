package store

import (
	"errors"
	"testing"
	"time"
)

func TestUserCreateAndLookup(t *testing.T) {
	st := openStore(t)
	ctx := t.Context()

	user := User{ID: NewID(), Username: "alice", PasswordHash: "hash", Role: RoleAdmin}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	byName, err := st.Users.ByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("ByUsername() error = %v", err)
	}
	if byName.ID != user.ID || byName.PasswordHash != "hash" || byName.Role != RoleAdmin {
		t.Fatalf("ByUsername() = %+v", byName)
	}
	if byName.Locale != DefaultLocale {
		t.Errorf("locale = %q, want %q", byName.Locale, DefaultLocale)
	}
	if byName.CreatedAt.IsZero() || byName.UpdatedAt == nil {
		t.Errorf("timestamps = %v / %v", byName.CreatedAt, byName.UpdatedAt)
	}
	if byName.Disabled() {
		t.Error("a new user must not be disabled")
	}

	byID, err := st.Users.ByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("ByID() error = %v", err)
	}
	if byID.Username != "alice" {
		t.Fatalf("ByID() = %+v", byID)
	}

	if _, err := st.Users.ByUsername(ctx, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ByUsername(unknown) error = %v, want ErrNotFound", err)
	}
	if _, err := st.Users.ByID(ctx, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ByID(unknown) error = %v, want ErrNotFound", err)
	}
}

func TestUserCreateRejectsADuplicateUsername(t *testing.T) {
	st := openStore(t)
	ctx := t.Context()

	if err := st.Users.Create(ctx, User{ID: NewID(), Username: "alice"}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	err := st.Users.Create(ctx, User{ID: NewID(), Username: "alice"})
	if !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("Create() duplicate error = %v, want ErrUsernameTaken", err)
	}
}

func TestUserSetters(t *testing.T) {
	st := openStore(t)
	ctx := t.Context()

	user := User{ID: NewID(), Username: "alice", PasswordHash: "old", Role: RoleUser}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := st.Users.SetPasswordHash(ctx, user.ID, "new"); err != nil {
		t.Fatalf("SetPasswordHash() error = %v", err)
	}
	if err := st.Users.SetRole(ctx, user.ID, RoleAdmin); err != nil {
		t.Fatalf("SetRole() error = %v", err)
	}
	if err := st.Users.SetLocale(ctx, user.ID, "en"); err != nil {
		t.Fatalf("SetLocale() error = %v", err)
	}
	if err := st.Users.SetDisabled(ctx, user.ID, true); err != nil {
		t.Fatalf("SetDisabled(true) error = %v", err)
	}

	stored, err := st.Users.ByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("ByID() error = %v", err)
	}
	if stored.PasswordHash != "new" || stored.Role != RoleAdmin || stored.Locale != "en" {
		t.Fatalf("user after setters = %+v", stored)
	}
	if !stored.Disabled() {
		t.Fatal("user is not disabled")
	}
	if stored.UpdatedAt == nil || stored.UpdatedAt.Before(stored.CreatedAt) {
		t.Errorf("updated_at = %v, created_at = %v", stored.UpdatedAt, stored.CreatedAt)
	}

	if err := st.Users.SetDisabled(ctx, user.ID, false); err != nil {
		t.Fatalf("SetDisabled(false) error = %v", err)
	}
	stored, err = st.Users.ByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("ByID() error = %v", err)
	}
	if stored.Disabled() {
		t.Fatal("user is still disabled")
	}

	if err := st.Users.SetRole(ctx, "nobody", RoleAdmin); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetRole(unknown) error = %v, want ErrNotFound", err)
	}
}

func TestUserListCountsAttempts(t *testing.T) {
	st := openStore(t)
	ctx := t.Context()

	local := localUser(t, st)
	for range 2 {
		if err := st.Attempts.Create(ctx, Attempt{
			ID:         NewID(),
			UserID:     local.ID,
			LabID:      "net-ip-01-link-down",
			LabVersion: 1,
			Mode:       "guided",
			ParamsJSON: "{}",
			Status:     StatusPassed,
		}); err != nil {
			t.Fatalf("Attempts.Create() error = %v", err)
		}
	}
	if err := st.Users.Create(ctx, User{ID: NewID(), Username: "alice", CreatedAt: time.Now().Add(time.Minute)}); err != nil {
		t.Fatalf("Users.Create() error = %v", err)
	}

	summaries, err := st.Users.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("got %d users, want 2", len(summaries))
	}
	if summaries[0].Username != LocalUsername || summaries[0].Attempts != 2 {
		t.Errorf("summaries[0] = %+v", summaries[0])
	}
	if summaries[1].Username != "alice" || summaries[1].Attempts != 0 {
		t.Errorf("summaries[1] = %+v", summaries[1])
	}

	count, err := st.Users.Count(ctx)
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if count != 2 {
		t.Errorf("Count() = %d, want 2", count)
	}
}
