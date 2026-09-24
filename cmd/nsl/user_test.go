package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/AmerDwight/network-skill-lab/internal/auth"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

func runUser(t *testing.T, password string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := userCmd(args, strings.NewReader(password), &out)
	return out.String(), err
}

func userDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("NSL_DATA_DIR", dir)
	return dir
}

func storedUser(t *testing.T, dir, username string) store.User {
	t.Helper()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	defer func() { _ = st.Close() }()

	user, err := st.Users.ByUsername(context.Background(), username)
	if err != nil {
		t.Fatalf("ByUsername() error = %v", err)
	}
	return user
}

func TestUserAddCreatesAnAdmin(t *testing.T) {
	dir := userDataDir(t)

	out, err := runUser(t, "secret123\n", "add", "alice", "--role", "admin", "--password-stdin")
	if err != nil {
		t.Fatalf("user add error = %v", err)
	}
	if !strings.Contains(out, "created user alice") {
		t.Errorf("output = %q", out)
	}

	user := storedUser(t, dir, "alice")
	if user.Role != store.RoleAdmin {
		t.Errorf("role = %q, want admin", user.Role)
	}
	if user.Disabled() {
		t.Error("a new user must not be disabled")
	}
	ok, err := auth.VerifyPassword(user.PasswordHash, "secret123")
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if !ok {
		t.Error("the stored hash does not match the password given on stdin")
	}
}

func TestUserAddDefaultsToTheUserRole(t *testing.T) {
	dir := userDataDir(t)

	if _, err := runUser(t, "secret123", "add", "bob", "--password-stdin"); err != nil {
		t.Fatalf("user add error = %v", err)
	}
	if role := storedUser(t, dir, "bob").Role; role != store.RoleUser {
		t.Errorf("role = %q, want user", role)
	}
}

func TestUserAddRejectsBadInput(t *testing.T) {
	userDataDir(t)

	if _, err := runUser(t, "secret123", "add", "Alice", "--password-stdin"); err == nil {
		t.Error("user add accepted an invalid username")
	}
	if _, err := runUser(t, "short", "add", "alice", "--password-stdin"); err == nil {
		t.Error("user add accepted a short password")
	}
	if _, err := runUser(t, "secret123", "add", "alice", "--role", "root", "--password-stdin"); err == nil {
		t.Error("user add accepted an unknown role")
	}
	if _, err := runUser(t, "secret123", "add", "--password-stdin"); err == nil {
		t.Error("user add accepted a missing name")
	}
	if _, err := runUser(t, "secret123", "add", "alice", "bob", "--password-stdin"); err == nil {
		t.Error("user add accepted two names")
	}

	if _, err := runUser(t, "secret123", "add", "alice", "--password-stdin"); err != nil {
		t.Fatalf("user add error = %v", err)
	}
	err := userCmd([]string{"add", "alice", "--password-stdin"}, strings.NewReader("secret123"), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("user add duplicate error = %v", err)
	}
}

func TestUserPasswdChangesThePassword(t *testing.T) {
	dir := userDataDir(t)

	if _, err := runUser(t, "secret123", "add", "alice", "--password-stdin"); err != nil {
		t.Fatalf("user add error = %v", err)
	}
	out, err := runUser(t, "newpass99", "passwd", "alice", "--password-stdin")
	if err != nil {
		t.Fatalf("user passwd error = %v", err)
	}
	if !strings.Contains(out, "alice") {
		t.Errorf("output = %q", out)
	}

	user := storedUser(t, dir, "alice")
	ok, err := auth.VerifyPassword(user.PasswordHash, "newpass99")
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if !ok {
		t.Error("the password was not changed")
	}

	if _, err := runUser(t, "newpass99", "passwd", "nobody", "--password-stdin"); err == nil {
		t.Error("user passwd accepted an unknown user")
	}
	if _, err := runUser(t, "short", "passwd", "alice", "--password-stdin"); err == nil {
		t.Error("user passwd accepted a short password")
	}
}

func TestUserDisableAndEnable(t *testing.T) {
	dir := userDataDir(t)

	if _, err := runUser(t, "secret123", "add", "alice", "--password-stdin"); err != nil {
		t.Fatalf("user add error = %v", err)
	}

	if _, err := runUser(t, "", "disable", "alice"); err != nil {
		t.Fatalf("user disable error = %v", err)
	}
	if !storedUser(t, dir, "alice").Disabled() {
		t.Fatal("the user was not disabled")
	}

	if _, err := runUser(t, "", "enable", "alice"); err != nil {
		t.Fatalf("user enable error = %v", err)
	}
	if storedUser(t, dir, "alice").Disabled() {
		t.Fatal("the user was not enabled again")
	}

	if _, err := runUser(t, "", "disable", "nobody"); err == nil {
		t.Error("user disable accepted an unknown user")
	}
	if _, err := runUser(t, "", "disable"); err == nil {
		t.Error("user disable accepted a missing name")
	}
}

func TestUserList(t *testing.T) {
	userDataDir(t)

	if _, err := runUser(t, "secret123", "add", "alice", "--role", "admin", "--password-stdin"); err != nil {
		t.Fatalf("user add error = %v", err)
	}
	if _, err := runUser(t, "", "disable", "alice"); err != nil {
		t.Fatalf("user disable error = %v", err)
	}

	out, err := runUser(t, "", "list")
	if err != nil {
		t.Fatalf("user list error = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("user list printed %d lines:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "USERNAME") {
		t.Errorf("header = %q", lines[0])
	}
	if !strings.Contains(lines[1], store.LocalUsername) {
		t.Errorf("first row = %q", lines[1])
	}
	if !strings.Contains(lines[2], "alice") || !strings.Contains(lines[2], "admin") {
		t.Errorf("second row = %q", lines[2])
	}
	if strings.HasSuffix(strings.TrimSpace(lines[2]), "-") {
		t.Errorf("the disabled column of a disabled user is empty: %q", lines[2])
	}

	if _, err := runUser(t, "", "list", "extra"); err == nil {
		t.Error("user list accepted an argument")
	}
}

func TestUserCmdRejectsUnknownCommands(t *testing.T) {
	userDataDir(t)

	if _, err := runUser(t, ""); err == nil {
		t.Error("user accepted a missing command")
	}
	if _, err := runUser(t, "", "remove", "alice"); err == nil {
		t.Error("user accepted an unknown command")
	}
}

func TestUserAddWithoutATerminalRequiresPasswordStdin(t *testing.T) {
	userDataDir(t)

	err := userCmd([]string{"add", "alice"}, strings.NewReader("secret123"), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--password-stdin") {
		t.Errorf("user add without a terminal error = %v", err)
	}
}
