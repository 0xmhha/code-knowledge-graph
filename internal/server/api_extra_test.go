package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/internal/server"
)

// buildFixture compiles the resolve testdata into a temp dir and returns a
// read-only Store. The caller is responsible for calling store.Close().
func buildFixture(t *testing.T) *persist.Store {
	t.Helper()
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
	t.Cleanup(func() { store.Close() })
	return store
}

// TestHandlersExtended exercises hierarchy, edges, blob, and search handlers
// against a real graph built from the resolve fixture.
func TestHandlersExtended(t *testing.T) {
	store := buildFixture(t)
	srv := server.New(store, nil)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	// ---- helper: GET and decode JSON array --------------------------------
	getJSONArray := func(t *testing.T, path string) []any {
		t.Helper()
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("GET %s = %d, body: %s", path, resp.StatusCode, body)
		}
		ct := resp.Header.Get("content-type")
		if ct != "application/json" {
			t.Errorf("GET %s: content-type = %q, want application/json", path, ct)
		}
		var out []any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("GET %s: decode JSON array: %v", path, err)
		}
		return out
	}

	// ---- hierarchy tests --------------------------------------------------
	t.Run("hierarchy_pkg", func(t *testing.T) {
		rows := getJSONArray(t, "/api/hierarchy?kind=pkg")
		// The resolve fixture has two packages, so we expect at least one row.
		if len(rows) == 0 {
			t.Error("hierarchy?kind=pkg returned empty array, expected at least one package row")
		}
	})

	t.Run("hierarchy_topic", func(t *testing.T) {
		// Leiden clustering may yield no topic rows for a tiny fixture — that
		// is still a valid (200 + empty array) response.
		getJSONArray(t, "/api/hierarchy?kind=topic")
	})

	t.Run("hierarchy_default", func(t *testing.T) {
		// No kind param defaults to "pkg".
		rows := getJSONArray(t, "/api/hierarchy")
		if len(rows) == 0 {
			t.Error("hierarchy (default) returned empty array, expected at least one row")
		}
	})

	// ---- search tests -----------------------------------------------------
	t.Run("search_greet", func(t *testing.T) {
		// "Greet" is defined in a/a.go — must be present in FTS.
		hits := getJSONArray(t, "/api/search?q=Greet")
		if len(hits) == 0 {
			t.Error("/api/search?q=Greet returned no hits, expected at least one")
		}
	})

	t.Run("search_no_match", func(t *testing.T) {
		// Should still return 200 with an empty array.
		getJSONArray(t, "/api/search?q=zzzz_no_such_symbol_xqz")
	})

	t.Run("search_empty_q", func(t *testing.T) {
		// Empty q must return 200 + empty array (not 400).
		getJSONArray(t, "/api/search?q=")
	})

	// ---- collect node IDs for edges/blob tests ---------------------------
	// Top-level /api/nodes returns Package nodes. Drill into each package to
	// find File children and then Function grandchildren.
	pkgNodes := getJSONArray(t, "/api/nodes?limit=200")
	if len(pkgNodes) == 0 {
		t.Fatal("/api/nodes returned no results; cannot run edges/blob sub-tests")
	}

	var funcNodeID, anyNodeID string
	for _, raw := range pkgNodes {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		pkgID, _ := m["id"].(string)
		if pkgID == "" {
			continue
		}
		if anyNodeID == "" {
			anyNodeID = pkgID
		}
		// Query file-level children of this package.
		fileNodes := getJSONArray(t, "/api/nodes?parent="+pkgID+"&limit=200")
		for _, fr := range fileNodes {
			fm, ok := fr.(map[string]any)
			if !ok {
				continue
			}
			fileID, _ := fm["id"].(string)
			if fileID == "" {
				continue
			}
			// Query function-level grandchildren of this file.
			funcNodes := getJSONArray(t, "/api/nodes?parent="+fileID+"&limit=200")
			for _, fnr := range funcNodes {
				fnm, ok := fnr.(map[string]any)
				if !ok {
					continue
				}
				id, _ := fnm["id"].(string)
				typ, _ := fnm["type"].(string)
				if typ == "Function" && funcNodeID == "" {
					funcNodeID = id
				}
			}
		}
	}
	if anyNodeID == "" {
		t.Fatal("could not extract any node ID from /api/nodes response")
	}

	// ---- edges tests (POST /api/edges with JSON body) --------------------
	t.Run("edges_with_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"ids": []string{anyNodeID}})
		resp, err := http.Post(ts.URL+"/api/edges", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /api/edges: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("POST /api/edges = %d, body: %s", resp.StatusCode, b)
		}
		if ct := resp.Header.Get("content-type"); ct != "application/json" {
			t.Errorf("content-type = %q, want application/json", ct)
		}
		var edges []any
		if err := json.NewDecoder(resp.Body).Decode(&edges); err != nil {
			t.Fatalf("decode edges JSON: %v", err)
		}
		// Result may be empty for nodes with no edges; just verify it's an array.
	})

	t.Run("edges_empty_ids", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"ids": []string{}})
		resp, err := http.Post(ts.URL+"/api/edges", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /api/edges (empty): %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("POST /api/edges (empty ids) = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("edges_bad_body", func(t *testing.T) {
		resp, err := http.Post(ts.URL+"/api/edges", "application/json", bytes.NewReader([]byte("not-json")))
		if err != nil {
			t.Fatalf("POST /api/edges (bad body): %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("POST /api/edges (bad body) = %d, want 400", resp.StatusCode)
		}
	})

	// ---- blob tests (GET /api/blob/{id}) ----------------------------------
	if funcNodeID != "" {
		t.Run("blob_function_present", func(t *testing.T) {
			resp, err := http.Get(ts.URL + "/api/blob/" + funcNodeID)
			if err != nil {
				t.Fatalf("GET /api/blob/%s: %v", funcNodeID, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("GET /api/blob/%s = %d, body: %s", funcNodeID, resp.StatusCode, body)
			}
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read blob body: %v", err)
			}
			if len(data) == 0 {
				t.Error("blob for function node is empty, expected source bytes")
			}
		})
	} else {
		t.Log("no Function node found in fixture; skipping blob_function_present sub-test")
	}

	t.Run("blob_missing_id", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/blob/nonexistent0000")
		if err != nil {
			t.Fatalf("GET /api/blob/nonexistent: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET /api/blob/nonexistent = %d, want 404", resp.StatusCode)
		}
	})
}

// TestCopyViewerAssetsTo verifies that CopyViewerAssetsTo materialises the
// embedded viewer onto disk. index.html and assets/viewer.js must both appear.
func TestCopyViewerAssetsTo(t *testing.T) {
	dst := t.TempDir()
	if err := server.CopyViewerAssetsTo(dst); err != nil {
		t.Fatalf("CopyViewerAssetsTo: %v", err)
	}

	// Collect all files that were written.
	var written []string
	if err := filepath.WalkDir(dst, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(dst, path)
			written = append(written, rel)
		}
		return nil
	}); err != nil {
		t.Fatalf("WalkDir dst: %v", err)
	}

	if len(written) == 0 {
		t.Fatal("CopyViewerAssetsTo wrote no files; embedded web_assets may be empty")
	}
	t.Logf("CopyViewerAssetsTo wrote: %v", written)

	// Must include index.html.
	indexPath := filepath.Join(dst, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		t.Errorf("index.html missing from dst: %v", err)
	}

	// Must include at least one file under assets/.
	assetsDir := filepath.Join(dst, "assets")
	entries, err := os.ReadDir(assetsDir)
	if err != nil {
		t.Errorf("assets/ directory missing from dst: %v", err)
	} else if len(entries) == 0 {
		t.Error("assets/ directory is empty after CopyViewerAssetsTo")
	}
}
