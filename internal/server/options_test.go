package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/server"
)

// TestOptions_NoViewer verifies that --no-viewer wiring drops the static
// mount: /api/* still answers, but /<anything> 404s instead of returning the
// embedded index. This is the contract reverse-proxy operators rely on.
func TestOptions_NoViewer(t *testing.T) {
	store := buildFixture(t)
	srv := server.NewWithOptions(store, nil, server.Options{NoViewer: true})
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/manifest")
	if err != nil {
		t.Fatalf("GET /api/manifest: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("api still required; got %d", resp.StatusCode)
	}

	resp, err = http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET / with NoViewer = %d, want 404", resp.StatusCode)
	}
}

// TestOptions_DevViewerDir verifies that an explicit dev dir overrides the
// embedded FS — the served / payload comes from disk so the viewer team can
// edit assets without rebuilding ckg.
func TestOptions_DevViewerDir(t *testing.T) {
	dir := t.TempDir()
	const marker = "DEV-VIEWER-MARKER-7c2f9"
	if err := os.WriteFile(filepath.Join(dir, "index.html"),
		[]byte("<html><body>"+marker+"</body></html>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}

	store := buildFixture(t)
	srv := server.NewWithOptions(store, nil, server.Options{DevViewerDir: dir})
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), marker) {
		t.Errorf("disk index not served; body = %q", body)
	}
}
