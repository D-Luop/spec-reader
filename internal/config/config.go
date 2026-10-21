package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Title    string `yaml:"title" json:"title"`
	CacheDir string `yaml:"cacheDir" json:"-"`
	Proxy    Proxy  `yaml:"proxy" json:"proxy"`
	Specs    []Spec `yaml:"specs" json:"specs"`
	Auth     Auth   `yaml:"auth" json:"auth,omitempty"`
}

type Proxy struct {
	TimeoutSeconds       int      `yaml:"timeoutSeconds" json:"-"`
	AllowPrivateNetworks bool     `yaml:"allowPrivateNetworks" json:"-"`
	ExtraOrigins         []string `yaml:"extraOrigins" json:"-"`
}

type Spec struct {
	ID         string        `yaml:"id" json:"id"`
	Label      string        `yaml:"label" json:"label,omitempty"`
	File       string        `yaml:"file" json:"-"`
	GitHub     *GitHubSource `yaml:"github" json:"github,omitempty"`
	Group      string        `yaml:"group" json:"group,omitempty"`
	Production bool          `yaml:"production" json:"production,omitempty"`
	TryIt      *bool         `yaml:"tryIt" json:"tryIt,omitempty"`
}

type GitHubSource struct {
	Repository string `yaml:"repository" json:"repository"`
	Ref        string `yaml:"ref" json:"ref"`
	Path       string `yaml:"path" json:"path"`
	TokenEnv   string `yaml:"tokenEnv" json:"-"`
}

type Auth struct {
	Presets []Preset `yaml:"presets" json:"presets,omitempty"`
}
type Preset struct {
	ID               string          `yaml:"id" json:"id"`
	Label            string          `yaml:"label" json:"label"`
	Schemes          []string        `yaml:"schemes" json:"schemes"`
	Fields           []Field         `yaml:"fields" json:"fields"`
	Login            RequestTemplate `yaml:"login" json:"login"`
	Refresh          RequestTemplate `yaml:"refresh" json:"refresh,omitempty"`
	TokenPath        string          `yaml:"tokenPath" json:"tokenPath"`
	RefreshTokenPath string          `yaml:"refreshTokenPath" json:"refreshTokenPath,omitempty"`
	ExpiresInPath    string          `yaml:"expiresInPath" json:"expiresInPath,omitempty"`
}
type Field struct {
	Name   string `yaml:"name" json:"name"`
	Label  string `yaml:"label" json:"label,omitempty"`
	Secret bool   `yaml:"secret" json:"secret,omitempty"`
}
type RequestTemplate struct {
	Method string         `yaml:"method" json:"method"`
	URL    string         `yaml:"url" json:"url"`
	Body   map[string]any `yaml:"body" json:"body,omitempty"`
}

func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if cfg.Title == "" {
		cfg.Title = "API Specs"
	}
	if cfg.Proxy.TimeoutSeconds == 0 {
		cfg.Proxy.TimeoutSeconds = 30
	}
	if cfg.CacheDir == "" {
		cfg.CacheDir = filepath.Join(os.TempDir(), "spec-reader-cache")
	} else if !filepath.IsAbs(cfg.CacheDir) {
		cfg.CacheDir = filepath.Join(filepath.Dir(path), cfg.CacheDir)
	}
	if cfg.Proxy.TimeoutSeconds < 1 || cfg.Proxy.TimeoutSeconds > int((2*time.Minute)/time.Second) {
		return Config{}, fmt.Errorf("proxy.timeoutSeconds must be between 1 and 120")
	}
	base := filepath.Dir(path)
	seen := map[string]bool{}
	for i := range cfg.Specs {
		s := &cfg.Specs[i]
		if s.ID == "" {
			return Config{}, fmt.Errorf("spec %d requires id", i+1)
		}
		if (s.File == "") == (s.GitHub == nil) {
			return Config{}, fmt.Errorf("spec %q requires exactly one of file or github", s.ID)
		}
		if seen[s.ID] {
			return Config{}, fmt.Errorf("duplicate spec id %q", s.ID)
		}
		seen[s.ID] = true
		if s.File != "" {
			if !filepath.IsAbs(s.File) {
				s.File = filepath.Join(base, s.File)
			}
			if _, err := os.Stat(s.File); err != nil {
				return Config{}, fmt.Errorf("spec %q: %w", s.ID, err)
			}
		} else {
			if err := validateGitHub(*s.GitHub); err != nil {
				return Config{}, fmt.Errorf("spec %q github: %w", s.ID, err)
			}
		}
	}
	for _, origin := range cfg.Proxy.ExtraOrigins {
		if err := validOrigin(origin); err != nil {
			return Config{}, fmt.Errorf("extra origin %q: %w", origin, err)
		}
	}
	return cfg, nil
}

func validateGitHub(source GitHubSource) error {
	parts := strings.Split(source.Repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("repository must be owner/name")
	}
	if source.Ref == "" || source.Path == "" || strings.HasPrefix(source.Path, "/") || strings.Contains(source.Path, "..") {
		return fmt.Errorf("ref and a relative path without '..' are required")
	}
	return nil
}

func validOrigin(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" {
		return fmt.Errorf("must be an http(s) origin")
	}
	return nil
}
