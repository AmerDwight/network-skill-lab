package api

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"testing"

	"github.com/AmerDwight/network-skill-lab/internal/auth"
	"github.com/AmerDwight/network-skill-lab/internal/content/contenttest"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

func cookieOf(t *testing.T, resp *http.Response, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range resp.Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("response carries no %s cookie", name)
	return nil
}

func (h *harness) anonymousClient() *http.Client {
	h.t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		h.t.Fatalf("cookie jar: %v", err)
	}
	return &http.Client{Jar: jar, Transport: h.server.Client().Transport}
}

func TestLoginSetsASessionCookie(t *testing.T) {
	h := newHarness(t)
	client := h.anonymousClient()

	resp := h.send(client, http.MethodPost, "/api/auth/login",
		fmt.Sprintf(`{"username":%q,"password":%q}`, testUsername, testPassword))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := decodeJSON(t, resp)
	requireKeys(t, body, "id", "username", "role", "locale", "created_at")
	if body["username"] != testUsername || body["role"] != store.RoleUser {
		t.Errorf("me = %v", body)
	}

	cookie := cookieOf(t, resp, sessionCookie)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Errorf("cookie = %+v", cookie)
	}
	if cookie.Secure {
		t.Error("cookie is Secure on a plain HTTP request")
	}

	me := decodeJSON(t, h.send(client, http.MethodGet, "/api/auth/me", ""))
	if me["id"] != h.user.ID {
		t.Errorf("me.id = %v, want %s", me["id"], h.user.ID)
	}
}

func TestLoginMarksTheCookieSecureBehindTLS(t *testing.T) {
	h := newHarness(t)
	client := h.anonymousClient()

	req := h.request(http.MethodPost, "/api/auth/login",
		fmt.Sprintf(`{"username":%q,"password":%q}`, testUsername, testPassword))
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if !cookieOf(t, resp, sessionCookie).Secure {
		t.Error("cookie is not Secure behind an https proxy")
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	h := newHarness(t)
	resp := h.send(h.anonymousClient(), http.MethodPost, "/api/auth/login",
		fmt.Sprintf(`{"username":%q,"password":"nope nope nope"}`, testUsername))
	requireError(t, resp, http.StatusUnauthorized, "invalid_credentials")
}

func TestLoginRejectsUnknownUserTheSameWay(t *testing.T) {
	h := newHarness(t)
	resp := h.send(h.anonymousClient(), http.MethodPost, "/api/auth/login",
		`{"username":"ghost","password":"nope nope nope"}`)
	requireError(t, resp, http.StatusUnauthorized, "invalid_credentials")
}

func TestLoginRejectsADisabledUser(t *testing.T) {
	h := newHarness(t)
	if err := h.store.Users.SetDisabled(t.Context(), h.other.ID, true); err != nil {
		t.Fatalf("disable: %v", err)
	}
	resp := h.send(h.anonymousClient(), http.MethodPost, "/api/auth/login",
		fmt.Sprintf(`{"username":%q,"password":%q}`, h.other.Username, testPassword))
	requireError(t, resp, http.StatusForbidden, "user_disabled")
}

func TestLoginRateLimitsRepeatedFailures(t *testing.T) {
	h := newHarness(t)
	client := h.anonymousClient()
	body := fmt.Sprintf(`{"username":%q,"password":"nope nope nope"}`, testUsername)

	for range auth.MaxLoginFailures {
		if resp := h.send(client, http.MethodPost, "/api/auth/login", body); resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 while the limit is not reached", resp.StatusCode)
		}
	}
	requireError(t, h.send(client, http.MethodPost, "/api/auth/login", body),
		http.StatusTooManyRequests, "too_many_attempts")
}

func TestLoginRateLimitIsPerClientIP(t *testing.T) {
	h := newHarness(t)
	client := h.anonymousClient()
	body := fmt.Sprintf(`{"username":%q,"password":"nope nope nope"}`, testUsername)

	for range auth.MaxLoginFailures {
		req := h.request(http.MethodPost, "/api/auth/login", body)
		req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		_ = resp.Body.Close()
	}

	blocked := h.request(http.MethodPost, "/api/auth/login", body)
	blocked.Header.Set("X-Forwarded-For", "203.0.113.7")
	resp, err := client.Do(blocked)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	requireError(t, resp, http.StatusTooManyRequests, "too_many_attempts")

	other := h.request(http.MethodPost, "/api/auth/login",
		fmt.Sprintf(`{"username":%q,"password":%q}`, testUsername, testPassword))
	other.Header.Set("X-Forwarded-For", "198.51.100.4")
	accepted, err := client.Do(other)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	t.Cleanup(func() { _ = accepted.Body.Close() })
	if accepted.StatusCode != http.StatusOK {
		t.Errorf("status for another client IP = %d, want 200", accepted.StatusCode)
	}
}

func TestLogoutClearsTheSession(t *testing.T) {
	h := newHarness(t)
	resp := h.do(http.MethodPost, "/api/auth/logout", "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if cookie := cookieOf(t, resp, sessionCookie); cookie.MaxAge >= 0 {
		t.Errorf("cookie MaxAge = %d, want it expired", cookie.MaxAge)
	}
	requireError(t, h.do(http.MethodGet, "/api/auth/me", ""), http.StatusUnauthorized, "unauthorized")
}

func TestMeRequiresASession(t *testing.T) {
	h := newHarness(t)
	requireError(t, h.send(h.anonymousClient(), http.MethodGet, "/api/auth/me", ""),
		http.StatusUnauthorized, "unauthorized")
}

func TestMeReportsNoUsersOnAFreshInstall(t *testing.T) {
	h := newBareHarness(t, contenttest.Dir(), 0)
	requireError(t, h.send(h.anonymousClient(), http.MethodGet, "/api/auth/me", ""),
		http.StatusUnauthorized, "no_users")
}

func TestPatchMeStoresTheLocale(t *testing.T) {
	h := newHarness(t)
	body := decodeJSON(t, h.do(http.MethodPatch, "/api/auth/me", `{"locale":"en"}`))
	if body["locale"] != "en" {
		t.Fatalf("locale = %v, want en", body["locale"])
	}
	stored, err := h.store.Users.ByID(t.Context(), h.user.ID)
	if err != nil {
		t.Fatalf("read user: %v", err)
	}
	if stored.Locale != "en" {
		t.Errorf("stored locale = %q, want en", stored.Locale)
	}
	requireError(t, h.do(http.MethodPatch, "/api/auth/me", `{"locale":"fr"}`), http.StatusBadRequest, "bad_request")
}

func TestDisabledUserIsRejectedByTheMiddleware(t *testing.T) {
	h := newHarness(t)
	client := h.login(h.other.Username, testPassword)
	if err := h.store.Users.SetDisabled(t.Context(), h.other.ID, true); err != nil {
		t.Fatalf("disable: %v", err)
	}
	requireError(t, h.send(client, http.MethodGet, "/api/auth/me", ""), http.StatusUnauthorized, "user_disabled")
}

func TestProtectedEndpointsNeedASession(t *testing.T) {
	h := newHarness(t)
	client := h.anonymousClient()
	paths := []string{"/api/labs", "/api/topics", "/api/docs", "/api/tracks",
		"/api/attempts/current", "/api/history", "/api/admin/stats"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			requireError(t, h.send(client, http.MethodGet, path, ""), http.StatusUnauthorized, "unauthorized")
		})
	}
}

func TestHealthStaysPublic(t *testing.T) {
	h := newHarness(t)
	if resp := h.send(h.anonymousClient(), http.MethodGet, "/api/health", ""); resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestWebSocketsRejectAnonymousCallersBeforeUpgrade(t *testing.T) {
	h := newHarness(t)
	id := h.running()

	conn, resp, err := dialAs(t, h, h.anonymousClient(), "/ws/attempts/"+id+"/events")
	if err == nil {
		_ = conn.CloseNow()
		t.Fatal("dial succeeded without a session")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("handshake response = %v", resp)
	}
	requireError(t, resp, http.StatusUnauthorized, "unauthorized")
}

func TestWritesMustCarryJSON(t *testing.T) {
	h := newHarness(t)
	req := h.request(http.MethodPost, "/api/progress", `{"kind":"doc","ref":"net/ip"}`)
	req.Header.Set("Content-Type", "text/plain")
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	requireError(t, resp, http.StatusUnsupportedMediaType, "unsupported_media_type")
}

func TestWritesWithoutABodyNeedNoContentType(t *testing.T) {
	h := newHarness(t)
	req := h.request(http.MethodPost, "/api/auth/logout", "")
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
}
