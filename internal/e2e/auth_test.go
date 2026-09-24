//go:build integration || content_integration

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AmerDwight/network-skill-lab/internal/auth"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

const e2ePassword = "correct horse"

func jsonRequest(t *testing.T, client *http.Client, method, url, body string) map[string]any {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, url, reader)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, url, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("send request %s %s: %v", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body of %s %s: %v", method, url, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode body %q: %v", raw, err)
	}
	decoded["_status"] = float64(resp.StatusCode)
	return decoded
}

func newLoggedInUser(t *testing.T, server *httptest.Server, st *store.Store, username string) (store.User, *http.Client) {
	t.Helper()
	hash, err := auth.HashPassword(e2ePassword)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user := store.User{ID: store.NewID(), Username: username, PasswordHash: hash, Role: store.RoleUser}
	if err := st.Users.Create(t.Context(), user); err != nil {
		t.Fatalf("create user %s: %v", username, err)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar, Transport: server.Client().Transport}
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, e2ePassword)
	if got := jsonRequest(t, client, http.MethodPost, server.URL+"/api/auth/login", body); got["_status"] != float64(http.StatusOK) {
		t.Fatalf("login as %s: %v", username, got)
	}
	return user, client
}
