package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestRouterMountsAPIBeforeTheSPA(t *testing.T) {
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("api " + r.URL.Path))
	})
	router := NewRouter(api)

	for _, path := range []string{"/api", "/api/labs", "/ws/attempts/01ABC/events"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if got, want := rec.Body.String(), "api "+path; got != want {
			t.Errorf("body for %s = %q, want %q", path, got, want)
		}
	}
}

func TestRouterAPINotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	NewRouter(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	if body.Error.Code != "not_found" {
		t.Errorf("error.code = %q, want %q", body.Error.Code, "not_found")
	}
	if body.Error.Message == "" {
		t.Error("error.message is empty")
	}
}

func TestSPA(t *testing.T) {
	dist := fstest.MapFS{
		"index.html":     {Data: []byte("<!doctype html><title>app</title>")},
		"assets/app.js":  {Data: []byte("console.log(1)")},
		"assets/app.css": {Data: []byte("body{}")},
	}
	handler := newSPA(dist)

	tests := []struct {
		name string
		path string
		want string
	}{
		{"root serves index", "/", "<!doctype html><title>app</title>"},
		{"asset served verbatim", "/assets/app.js", "console.log(1)"},
		{"unknown route falls back to index", "/attempts/01ABC", "<!doctype html><title>app</title>"},
		{"directory falls back to index", "/assets", "<!doctype html><title>app</title>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if rec.Body.String() != tt.want {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.want)
			}
		})
	}
}

func TestSPAWithoutIndex(t *testing.T) {
	rec := httptest.NewRecorder()
	newSPA(fstest.MapFS{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
