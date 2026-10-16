package store

import (
	"errors"
	"slices"
	"testing"
	"time"
)

func TestRunnerLifecycle(t *testing.T) {
	st := openStore(t)
	ctx := t.Context()

	runner := Runner{ID: NewID(), Name: "lab-01", TokenHash: "hash", Capabilities: []string{"container"}, Capacity: 3}
	if err := st.Runners.Create(ctx, runner); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := st.Runners.Create(ctx, Runner{ID: NewID(), Name: "lab-01", TokenHash: "other"}); !errors.Is(err, ErrRunnerNameTaken) {
		t.Fatalf("Create(duplicate) error = %v, want ErrRunnerNameTaken", err)
	}

	got, err := st.Runners.ByName(ctx, "lab-01")
	if err != nil {
		t.Fatalf("ByName() error = %v", err)
	}
	if got.ID != runner.ID || got.TokenHash != "hash" || got.Capacity != 3 || !slices.Equal(got.Capabilities, []string{"container"}) {
		t.Fatalf("ByName() = %+v", got)
	}
	if got.LastSeenAt != nil || got.Revoked() || got.Version != "" || got.CreatedAt.IsZero() {
		t.Fatalf("new runner = %+v", got)
	}

	seen := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	if err := st.Runners.Touch(ctx, runner.ID, seen, "v1.2.3", 5, []string{"container", "vm"}); err != nil {
		t.Fatalf("Touch() error = %v", err)
	}
	got, err = st.Runners.ByName(ctx, "lab-01")
	if err != nil {
		t.Fatalf("ByName() error = %v", err)
	}
	if got.LastSeenAt == nil || !got.LastSeenAt.Equal(seen) || got.Version != "v1.2.3" || got.Capacity != 5 || len(got.Capabilities) != 2 {
		t.Fatalf("after Touch() = %+v", got)
	}

	if err := st.Runners.Revoke(ctx, runner.ID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	got, _ = st.Runners.ByName(ctx, "lab-01")
	if !got.Revoked() {
		t.Fatal("runner is not revoked")
	}
	revokedAt := *got.RevokedAt
	if err := st.Runners.Revoke(ctx, runner.ID); err != nil {
		t.Fatalf("Revoke() again error = %v", err)
	}
	got, _ = st.Runners.ByName(ctx, "lab-01")
	if !got.RevokedAt.Equal(revokedAt) {
		t.Errorf("revoking twice moved revoked_at from %v to %v", revokedAt, got.RevokedAt)
	}

	if err := st.Runners.Create(ctx, Runner{ID: NewID(), Name: "lab-00", TokenHash: "h"}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	list, err := st.Runners.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 2 || list[0].Name != "lab-00" || list[1].Name != "lab-01" {
		t.Fatalf("List() = %+v", list)
	}
	if list[0].Capabilities == nil {
		t.Error("capabilities of a runner created without any should be empty, not nil")
	}

	if err := st.Runners.Delete(ctx, runner.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := st.Runners.ByName(ctx, "lab-01"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ByName(deleted) error = %v, want ErrNotFound", err)
	}
	for name, err := range map[string]error{
		"Touch":  st.Runners.Touch(ctx, "missing", seen, "", 0, nil),
		"Revoke": st.Runners.Revoke(ctx, "missing"),
		"Delete": st.Runners.Delete(ctx, "missing"),
	} {
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("%s(missing) error = %v, want ErrNotFound", name, err)
		}
	}
}
