package server

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/D-Luop/spec-reader/internal/config"
	"github.com/D-Luop/spec-reader/internal/specs"
	"github.com/go-chi/chi/v5"
)

//go:embed all:dist
var assets embed.FS

type api struct {
	cfg   config.Config
	proxy *proxy
	specs *specs.Store
}

func New(cfg config.Config) (http.Handler, error) {
	p, err := newProxy(cfg)
	if err != nil {
		return nil, err
	}
	store, err := specs.New(context.Background(), cfg)
	if err != nil {
		return nil, err
	}
	a := &api{cfg: cfg, proxy: p, specs: store}
	r := chi.NewRouter()
	r.Use(securityHeaders)
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	r.Get("/config.json", a.config)
	r.Get("/specs/{id}", a.spec)
	r.Post("/api/specs/{id}/refresh", a.refreshSpec)
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
	body, ok := a.specs.Get(id)
	if !ok {
		http.Error(w, "spec not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(body)
}

func (a *api) refreshSpec(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Spec-Reader") != "1" || !sameOrigin(r) {
		http.Error(w, "same-origin refresh required", http.StatusForbidden)
		return
	}
	changed, err := a.specs.Refresh(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "spec not found", http.StatusNotFound)
			return
		}
		http.Error(w, "refresh failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"changed": changed})
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
