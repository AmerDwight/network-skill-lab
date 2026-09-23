package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"
)

//go:embed all:dist
var assets embed.FS

func NewRouter(api http.Handler) *chi.Mux {
	r := chi.NewRouter()
	if api == nil {
		api = http.HandlerFunc(notFound)
	}
	r.Handle("/api", api)
	r.Handle("/api/*", api)
	r.Handle("/ws/*", api)
	r.Handle("/*", newSPA(dist()))
	return r
}

func dist() fs.FS {
	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}

func notFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("no route for %s %s", r.Method, r.URL.Path))
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	body := struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{}
	body.Error.Code = code
	body.Error.Message = message
	_ = json.NewEncoder(w).Encode(body)
}

type spa struct {
	dist  fs.FS
	files http.Handler
	index []byte
}

func newSPA(dist fs.FS) *spa {
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		index = nil
	}
	return &spa{dist: dist, files: http.FileServerFS(dist), index: index}
}

func (s *spa) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if name := strings.TrimPrefix(path.Clean(r.URL.Path), "/"); name != "" && name != "." {
		if info, err := fs.Stat(s.dist, name); err == nil && !info.IsDir() {
			s.files.ServeHTTP(w, r)
			return
		}
	}
	if s.index == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(s.index)
}
