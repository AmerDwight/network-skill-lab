package store

import (
	"testing"
	"time"
)

func createAttempt(t *testing.T, st *Store, userID, status string, createdAt time.Time) Attempt {
	t.Helper()
	attempt := Attempt{
		ID:         NewID(),
		UserID:     userID,
		LabID:      "net-ip-01-link-down",
		LabVersion: 1,
		Mode:       "guided",
		ParamsJSON: "{}",
		Status:     status,
		CreatedAt:  createdAt,
	}
	if err := st.Attempts.Create(t.Context(), attempt); err != nil {
		t.Fatalf("Attempts.Create() error = %v", err)
	}
	return attempt
}

func TestAttemptsListByUser(t *testing.T) {
	st := openStore(t)
	ctx := t.Context()

	local := localUser(t, st)
	other := User{ID: NewID(), Username: "alice"}
	if err := st.Users.Create(ctx, other); err != nil {
		t.Fatalf("Users.Create() error = %v", err)
	}

	base := time.Now().Add(-time.Hour)
	first := createAttempt(t, st, local.ID, StatusPassed, base)
	second := createAttempt(t, st, local.ID, StatusPassed, base.Add(time.Minute))
	third := createAttempt(t, st, local.ID, StatusRunning, base.Add(2*time.Minute))
	createAttempt(t, st, other.ID, StatusPassed, base.Add(3*time.Minute))

	page, err := st.Attempts.ListByUser(ctx, local.ID, time.Time{}, 10)
	if err != nil {
		t.Fatalf("ListByUser() error = %v", err)
	}
	if len(page) != 3 {
		t.Fatalf("got %d attempts, want 3", len(page))
	}
	if page[0].ID != third.ID || page[1].ID != second.ID || page[2].ID != first.ID {
		t.Fatalf("attempts are not in descending order: %v %v %v", page[0].ID, page[1].ID, page[2].ID)
	}

	limited, err := st.Attempts.ListByUser(ctx, local.ID, time.Time{}, 2)
	if err != nil {
		t.Fatalf("ListByUser() error = %v", err)
	}
	if len(limited) != 2 || limited[1].ID != second.ID {
		t.Fatalf("limited page = %+v", limited)
	}

	next, err := st.Attempts.ListByUser(ctx, local.ID, limited[1].CreatedAt, 2)
	if err != nil {
		t.Fatalf("ListByUser() error = %v", err)
	}
	if len(next) != 1 || next[0].ID != first.ID {
		t.Fatalf("next page = %+v", next)
	}

	empty, err := st.Attempts.ListByUser(ctx, "nobody", time.Time{}, 10)
	if err != nil {
		t.Fatalf("ListByUser() error = %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("got %d attempts for an unknown user", len(empty))
	}
}

func TestAttemptsCountActive(t *testing.T) {
	st := openStore(t)
	ctx := t.Context()

	local := localUser(t, st)
	other := User{ID: NewID(), Username: "alice"}
	if err := st.Users.Create(ctx, other); err != nil {
		t.Fatalf("Users.Create() error = %v", err)
	}

	now := time.Now()
	createAttempt(t, st, local.ID, StatusProvisioning, now)
	createAttempt(t, st, other.ID, StatusRunning, now)
	createAttempt(t, st, local.ID, StatusPassed, now)
	createAttempt(t, st, local.ID, StatusError, now)

	count, err := st.Attempts.CountActive(ctx)
	if err != nil {
		t.Fatalf("CountActive() error = %v", err)
	}
	if count != 2 {
		t.Errorf("CountActive() = %d, want 2", count)
	}
}
