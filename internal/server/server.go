package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/D-Luop/spec-reader/internal/config"
	"github.com/go-chi/chi/v5"
)

//go:embed all:dist
var assets embed.FS

type api struct {
	cfg   config.Config
	proxy *proxy
}

func New(cfg config.Config) (http.Handler, error) {
	p, err := newProxy(cfg)
	if err != nil {
		return nil, err
	}
	a := &api{cfg: cfg, proxy: p}
	r := chi.NewRouter()
	r.Use(securityHeaders)
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	r.Get("/config.json", a.config)
	r.Get("/specs/{id}", a.spec)
	r.Post("/api/proxy", a.proxy.handle)
	web, err := fs.Sub(assets, "dist")
	if err != nil {
		return nil, err
	}
	files := http.FileServer(http.FS(web))
	r.Handle("/*", spa(files, web))
	return r, nil
}

func (a *api) config(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(a.cfg)
}

func (a *api) spec(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	for _, spec := range a.cfg.Specs {
		if spec.ID != id {
			continue
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		http.ServeFile(w, r, spec.File)
		return
	}
	http.Error(w, "spec not found", http.StatusNotFound)
}

func spa(files http.Handler, web fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "." {
			if _, err := fs.Stat(web, name); err == nil {
				files.ServeHTTP(w, r)
				return
			}
		}
		b, err := fs.ReadFile(web, "index.html")
		if err != nil {
			http.Error(w, fmt.Sprintf("UI unavailable: %v", err), 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; img-src 'self' data:")
		next.ServeHTTP(w, r)
	})
}
