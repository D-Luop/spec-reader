package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D-Luop/spec-reader/internal/config"
)

func TestRefreshLocalSpecAndRejectCrossOrigin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openapi.yaml")
	if err := os.WriteFile(path, []byte("openapi: 3.1.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := New(config.Config{CacheDir: t.TempDir(), Specs: []config.Spec{{ID: "demo", File: path}}, Proxy: config.Proxy{TimeoutSeconds: 30}})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "https://reader.example/api/specs/demo/refresh", nil)
	request.Header.Set("Origin", "https://evil.example")
	request.Header.Set("X-Spec-Reader", "1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d", response.Code)
	}

	if err := os.WriteFile(path, []byte("openapi: 3.1.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodPost, "https://reader.example/api/specs/demo/refresh", nil)
	request.Header.Set("Origin", "https://reader.example")
	request.Header.Set("X-Spec-Reader", "1")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"changed":true`) {
		t.Fatalf("refresh status/body = %d %s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "https://reader.example/specs/demo", nil))
	if response.Code != http.StatusOK || response.Body.String() != "openapi: 3.1.1\n" {
		t.Fatalf("spec status/body = %d %q", response.Code, response.Body.String())
	}
}
