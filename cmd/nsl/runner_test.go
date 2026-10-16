package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AmerDwight/network-skill-lab/internal/auth"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

func runRunner(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := runnerCmd(args, &out, &errOut)
	return out.String(), err
}

func TestRunnerTokenRevokeList(t *testing.T) {
	dir := userDataDir(t)

	out, err := runRunner(t, "token", "lab-01", "--capabilities", "container, vm", "--capacity", "5")
	if err != nil {
		t.Fatalf("runner token error = %v", err)
	}
	token := strings.TrimSpace(out)
	if len(token) != 43 {
		t.Fatalf("stdout = %q, want only the token", out)
	}

	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	record, err := st.Runners.ByName(context.Background(), "lab-01")
	_ = st.Close()
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := auth.VerifyPassword(record.TokenHash, token); err != nil || !ok {
		t.Errorf("stored hash does not verify the printed token: %v", err)
	}
	if strings.Join(record.Capabilities, ",") != "container,vm" || record.Capacity != 5 {
		t.Errorf("record = %+v", record)
	}

	if _, err := runRunner(t, "token", "lab-01"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("duplicate token error = %v", err)
	}
	if _, err := runRunner(t, "token", "Bad Name"); err == nil {
		t.Error("an invalid name was accepted")
	}

	out, err = runRunner(t, "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "lab-01") || !strings.Contains(out, "container,vm") {
		t.Errorf("list = %q", out)
	}

	if out, err := runRunner(t, "revoke", "lab-01"); err != nil || !strings.Contains(out, "revoked runner lab-01") {
		t.Errorf("revoke = %q, %v", out, err)
	}
	if _, err := runRunner(t, "revoke", "nobody"); err == nil || !strings.Contains(err.Error(), "no such runner") {
		t.Errorf("revoke unknown error = %v", err)
	}
	out, _ = runRunner(t, "list")
	if lines := strings.Split(strings.TrimSpace(out), "\n"); len(lines) != 2 || strings.HasSuffix(lines[1], "-") {
		t.Errorf("list after revoke = %q", out)
	}
}

func TestRunnerServeValidatesFlags(t *testing.T) {
	t.Setenv(runnerTokenEnv, "")
	cases := map[string][]string{
		"missing url":       {"--name", "lab-01"},
		"missing name":      {"--url", "ws://x/ws/runner"},
		"bad provider":      {"--url", "ws://x", "--name", "lab-01", "--provider", "vm"},
		"zero capacity":     {"--url", "ws://x", "--name", "lab-01", "--max-sandboxes", "0"},
		"no token":          {"--url", "ws://x", "--name", "lab-01"},
		"missing tokenfile": {"--url", "ws://x", "--name", "lab-01", "--token-file", filepath.Join(t.TempDir(), "none")},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := runRunner(t, args...); err == nil {
				t.Errorf("runner %v succeeded", args)
			}
		})
	}
}

func TestReadRunnerToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("  secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(runnerTokenEnv, "from-env")
	if got, err := readRunnerToken(path); err != nil || got != "secret" {
		t.Errorf("readRunnerToken(file) = %q, %v", got, err)
	}
	if got, err := readRunnerToken(""); err != nil || got != "from-env" {
		t.Errorf("readRunnerToken(env) = %q, %v", got, err)
	}
}
