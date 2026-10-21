package specs

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/D-Luop/spec-reader/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGitHubRefreshUsesETag(t *testing.T) {
	requests := 0
	store := &Store{
		cacheDir: t.TempDir(),
		entries: map[string]*entry{"demo": {spec: config.Spec{ID: "demo", GitHub: &config.GitHubSource{
			Repository: "D-Luop/example", Ref: "feature/test", Path: "docs/openapi.yaml",
		}}}},
	}
	store.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Query().Get("ref") != "feature/test" || !strings.HasSuffix(r.URL.Path, "/contents/docs/openapi.yaml") {
			t.Fatalf("unexpected request URL: %s", r.URL)
		}
		if requests == 2 {
			if got := r.Header.Get("If-None-Match"); got != `"version-1"` {
				t.Fatalf("If-None-Match = %q", got)
			}
			return &http.Response{StatusCode: http.StatusNotModified, Status: "304 Not Modified", Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
		}
		body := base64.StdEncoding.EncodeToString([]byte("openapi: 3.1.0\n"))
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"encoding":"base64","content":%q}`, body))), Header: http.Header{"Etag": []string{`"version-1"`}}}, nil
	})}

	changed, err := store.Refresh(context.Background(), "demo")
	if err != nil || !changed {
		t.Fatalf("first refresh: changed=%v err=%v", changed, err)
	}
	changed, err = store.Refresh(context.Background(), "demo")
	if err != nil || changed {
		t.Fatalf("conditional refresh: changed=%v err=%v", changed, err)
	}
	if got, ok := store.Get("demo"); !ok || string(got) != "openapi: 3.1.0\n" {
		t.Fatalf("cached body = %q, %v", got, ok)
	}
}
