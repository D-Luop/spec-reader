package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadResolvesSpecsAndDefaults(t *testing.T) {
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "api.yaml"), []byte("openapi: 3.1.0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "config.yaml"), []byte("specs:\n  - id: demo\n    file: api.yaml\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(filepath.Join(d, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Title != "API Specs" || cfg.Proxy.TimeoutSeconds != 30 {
		t.Fatalf("defaults not applied: %#v", cfg)
	}
	if cfg.Specs[0].File != filepath.Join(d, "api.yaml") {
		t.Fatalf("file not resolved: %s", cfg.Specs[0].File)
	}
}

func TestLoadRejectsMissingSpec(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "config.yaml")
	if err := os.WriteFile(p, []byte("specs:\n  - id: nope\n    file: missing.yaml\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("expected missing spec error")
	}
}
