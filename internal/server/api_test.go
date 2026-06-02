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
	defer func() { _ = store.Close() }()

	srv := server.New(store, nil)
	ts := httptest.NewServer(srv)
	defer func() { ts.Close() }()

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
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("GET %s = %d, want 200", c.path, resp.StatusCode)
			}
			if got := resp.Header.Get("content-type"); got != "application/json" {
				t.Errorf("content-type = %q, want application/json", got)
			}
		})
	}

	// /api/evidence wiring smokes — the endpoint must surface the
	// allow-list guards added with mode=and. Both negative paths
	// (missing intent+issue, unknown mode) must return 400 so callers
	// don't silently fall back to a permissive query that returns
	// arbitrary commits. Lives next to TestHandlersBasic so the
	// fixture build is shared.
	t.Run("evidence_no_intent_no_issue_returns_400", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/evidence")
		if err != nil {
			t.Fatalf("GET /api/evidence: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	})
	t.Run("evidence_invalid_mode_returns_400", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/evidence?intent=anything&mode=xor")
		if err != nil {
			t.Fatalf("GET /api/evidence?mode=xor: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400 (mode allow-list failure)", resp.StatusCode)
		}
	})
	t.Run("evidence_mode_and_returns_200", func(t *testing.T) {
		// On the resolve fixture there's no git history, so the
		// EvidencePack is empty — that's the contract for non-git
		// graphs. We only assert the wiring is happy (200, JSON).
		resp, err := http.Get(ts.URL + "/api/evidence?intent=anything&mode=and")
		if err != nil {
			t.Fatalf("GET /api/evidence?mode=and: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200 (wiring sanity for mode=and)", resp.StatusCode)
		}
	})
}
