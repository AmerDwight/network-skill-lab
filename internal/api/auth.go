package api

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"strings"

	"github.com/AmerDwight/network-skill-lab/internal/auth"
	"github.com/AmerDwight/network-skill-lab/internal/store"
)

const sessionCookie = "nsl_session"

type userContextKey struct{}

func userOf(r *http.Request) store.User {
	user, _ := r.Context().Value(userContextKey{}).(store.User)
	return user
}

func (s *server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			s.unauthorized(w, r, "no session cookie")
			return
		}
		user, err := s.auth.Authenticate(r.Context(), cookie.Value)
		switch {
		case errors.Is(err, auth.ErrUserDisabled):
			clearSessionCookie(w, r)
			writeError(w, http.StatusUnauthorized, "user_disabled", "this account is disabled")
			return
		case errors.Is(err, store.ErrNotFound):
			clearSessionCookie(w, r)
			s.unauthorized(w, r, "the session has expired")
			return
		case err != nil:
			s.fail(w, "", err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey{}, user)))
	})
}

func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if userOf(r).Role != store.RoleAdmin {
			writeError(w, http.StatusForbidden, "forbidden", "this endpoint is for administrators")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func jsonWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			if r.ContentLength != 0 && !isJSON(r.Header.Get("Content-Type")) {
				writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type",
					"write requests must carry a JSON body")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isJSON(header string) bool {
	mediaType, _, err := mime.ParseMediaType(header)
	return err == nil && mediaType == "application/json"
}

func (s *server) unauthorized(w http.ResponseWriter, r *http.Request, message string) {
	if r.Method == http.MethodGet && r.URL.Path == "/api/auth/me" {
		if count, err := s.store.Users.CountEnabled(r.Context()); err == nil && count == 0 {
			writeError(w, http.StatusUnauthorized, "no_users", "no account exists yet; run `nsl user add` on the server")
			return
		}
	}
	writeError(w, http.StatusUnauthorized, "unauthorized", message)
}

func secureCookies(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, id string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    id,
		Path:     "/",
		MaxAge:   int(auth.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   secureCookies(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secureCookies(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		if hop := strings.TrimSpace(first); hop != "" {
			return hop
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		s.fail(w, "", err)
		return
	}

	ctx := auth.WithClientKey(r.Context(), clientIP(r))
	session, user, err := s.auth.Login(ctx, body.Username, body.Password, r.UserAgent())
	switch {
	case errors.Is(err, auth.ErrTooManyAttempts):
		writeError(w, http.StatusTooManyRequests, "too_many_attempts", "too many failed logins; try again in a minute")
		return
	case errors.Is(err, auth.ErrUserDisabled):
		writeError(w, http.StatusForbidden, "user_disabled", "this account is disabled")
		return
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "wrong username or password")
		return
	case err != nil:
		s.fail(w, "", err)
		return
	}

	setSessionCookie(w, r, session.ID)
	writeJSON(w, http.StatusOK, meOf(user))
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		if err := s.auth.Logout(r.Context(), cookie.Value); err != nil {
			s.fail(w, "", err)
			return
		}
	}
	clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, meOf(userOf(r)))
}

func (s *server) updateMe(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Locale string `json:"locale"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		s.fail(w, "", err)
		return
	}
	if body.Locale != "zh" && body.Locale != "en" {
		s.fail(w, "", fmt.Errorf("%w: locale must be zh or en, got %q", errBadRequest, body.Locale))
		return
	}

	user := userOf(r)
	if err := s.store.Users.SetLocale(r.Context(), user.ID, body.Locale); err != nil {
		s.fail(w, "", err)
		return
	}
	user.Locale = body.Locale
	writeJSON(w, http.StatusOK, meOf(user))
}
