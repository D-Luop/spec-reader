package specs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/D-Luop/spec-reader/internal/config"
)

const maxSpecSize = 10 << 20

type entry struct {
	spec config.Spec
	body []byte
	etag string
}

type Store struct {
	mu       sync.RWMutex
	entries  map[string]*entry
	cacheDir string
	client   *http.Client
}

type githubResponse struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

func New(ctx context.Context, cfg config.Config) (*Store, error) {
	store := &Store{
		entries:  make(map[string]*entry, len(cfg.Specs)),
		cacheDir: cfg.CacheDir,
		client:   &http.Client{Timeout: 20 * time.Second},
	}
	if err := os.MkdirAll(store.cacheDir, 0o700); err != nil {
		return nil, fmt.Errorf("create spec cache: %w", err)
	}
	for _, spec := range cfg.Specs {
		store.entries[spec.ID] = &entry{spec: spec}
		if spec.File != "" {
			body, err := os.ReadFile(spec.File)
			if err != nil {
				return nil, fmt.Errorf("load spec %q: %w", spec.ID, err)
			}
			store.entries[spec.ID].body = body
			continue
		}
		if _, err := store.refresh(ctx, spec.ID, false); err != nil {
			body, cacheErr := os.ReadFile(store.cachePath(spec.ID))
			if cacheErr != nil {
				return nil, fmt.Errorf("fetch spec %q: %w", spec.ID, err)
			}
			store.entries[spec.ID].body = body
		}
	}
	return store, nil
}

func (s *Store) Get(id string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[id]
	if !ok || len(e.body) == 0 {
		return nil, false
	}
	return append([]byte(nil), e.body...), true
}

// Refresh conditionally fetches a GitHub-backed spec. changed is false for a
// GitHub 304 response. Local specs are re-read from disk.
func (s *Store) Refresh(ctx context.Context, id string) (changed bool, err error) {
	return s.refresh(ctx, id, true)
}

func (s *Store) refresh(ctx context.Context, id string, conditional bool) (bool, error) {
	s.mu.RLock()
	e, ok := s.entries[id]
	if !ok {
		s.mu.RUnlock()
		return false, os.ErrNotExist
	}
	spec, etag := e.spec, e.etag
	s.mu.RUnlock()

	if spec.File != "" {
		body, err := os.ReadFile(spec.File)
		if err != nil {
			return false, err
		}
		s.mu.Lock()
		changed := string(s.entries[id].body) != string(body)
		s.entries[id].body = body
		s.mu.Unlock()
		return changed, nil
	}

	ownerRepo := strings.SplitN(spec.GitHub.Repository, "/", 2)
	requestURL := "https://api.github.com/repos/" + url.PathEscape(ownerRepo[0]) + "/" + url.PathEscape(ownerRepo[1]) + "/contents/" + escapePath(spec.GitHub.Path) + "?ref=" + url.QueryEscape(spec.GitHub.Ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "D-Luop/spec-reader")
	if token := os.Getenv(spec.GitHub.TokenEnv); spec.GitHub.TokenEnv != "" && token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if conditional && etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return false, fmt.Errorf("github returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	var payload githubResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxSpecSize*2)).Decode(&payload); err != nil {
		return false, fmt.Errorf("decode github response: %w", err)
	}
	if payload.Encoding != "base64" {
		return false, fmt.Errorf("unsupported github content encoding %q", payload.Encoding)
	}
	body, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(payload.Content, "\n", ""))
	if err != nil {
		return false, fmt.Errorf("decode github content: %w", err)
	}
	if len(body) > maxSpecSize {
		return false, fmt.Errorf("spec exceeds %d bytes", maxSpecSize)
	}
	if err := atomicWrite(s.cachePath(id), body); err != nil {
		return false, fmt.Errorf("cache spec: %w", err)
	}
	s.mu.Lock()
	changed := string(s.entries[id].body) != string(body)
	s.entries[id].body = body
	s.entries[id].etag = resp.Header.Get("ETag")
	s.mu.Unlock()
	return changed, nil
}

func (s *Store) cachePath(id string) string {
	return filepath.Join(s.cacheDir, id+".yaml")
}

func escapePath(value string) string {
	parts := strings.Split(value, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func atomicWrite(path string, body []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".spec-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
