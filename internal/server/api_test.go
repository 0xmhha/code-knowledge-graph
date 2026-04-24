package server_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/internal/server"
)

// TestHandlersBasic builds a real graph from the Go resolve fixture, opens
// the resulting graph.db read-only, and exercises the server end-to-end via
// httptest. The intent is a smoke test for the wiring (mux + Store helpers
// + JSON), not exhaustive handler coverage.
func TestHandlersBasic(t *testing.T) {
	out := t.TempDir()
	if _, err := buildpipe.Run(buildpipe.Options{
		SrcRoot:    "../parse/golang/testdata/resolve",
		OutDir:     out,
		Languages:  []string{"auto"},
		CKGVersion: "test",
	}); err != nil {
		t.Fatalf("buildpipe.Run: %v", err)
	}

	store, err := persist.OpenReadOnly(filepath.Join(out, "graph.db"))
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	defer store.Close()

	srv := server.New(store, nil)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	cases := []struct {
		name, path string
	}{
		{"manifest", "/api/manifest"},
		{"nodes", "/api/nodes?limit=10"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp, err := http.Get(ts.URL + c.path)
			if err != nil {
				t.Fatalf("GET %s: %v", c.path, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("GET %s = %d, want 200", c.path, resp.StatusCode)
			}
			if got := resp.Header.Get("content-type"); got != "application/json" {
				t.Errorf("content-type = %q, want application/json", got)
			}
		})
	}
}
