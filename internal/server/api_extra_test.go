package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
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
func buildFixture(t *testing.T) persist.Store {
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
	t.Cleanup(func() { _ = store.Close() })
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
		defer func() { _ = resp.Body.Close() }()
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

	// ---- community decoration --------------------------------------------
	// /api/nodes responses are wrapped with community_id + topic_label when
	// the topic_tree has data at defaultTopicResolution. The resolve fixture
	// exercises BuildTopicTree at 3 gammas so resolution=1 should produce
	// at least one labeled assignment, but Leiden on a tiny graph can also
	// land everything in singleton (unlabeled) communities — we tolerate
	// the empty case rather than flake.
	t.Run("nodes_community_decoration", func(t *testing.T) {
		nodes := getJSONArray(t, "/api/nodes?limit=200")
		if len(nodes) == 0 {
			t.Fatal("/api/nodes returned no results")
		}
		var sawCommunity bool
		for _, raw := range nodes {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			// community_id is omitted for nodes outside the topic_tree —
			// so its absence is fine, but if present it must be a number
			// and topic_label must be a non-empty string.
			cid, hasCID := m["community_id"]
			if !hasCID {
				continue
			}
			sawCommunity = true
			if _, ok := cid.(float64); !ok {
				t.Errorf("community_id has wrong type: %T (%v)", cid, cid)
			}
			label, _ := m["topic_label"].(string)
			if label == "" {
				t.Error("community_id present but topic_label empty")
			}
		}
		// Log-only: when the fixture happens to produce labels we want to
		// know the path was actually exercised. Not a hard assertion.
		t.Logf("community decoration observed on at least one node: %v", sawCommunity)
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
		defer func() { _ = resp.Body.Close() }()
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
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("POST /api/edges (empty ids) = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("edges_bad_body", func(t *testing.T) {
		resp, err := http.Post(ts.URL+"/api/edges", "application/json", bytes.NewReader([]byte("not-json")))
		if err != nil {
			t.Fatalf("POST /api/edges (bad body): %v", err)
		}
		_ = resp.Body.Close()

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
			defer func() { _ = resp.Body.Close() }()
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
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET /api/blob/nonexistent = %d, want 404", resp.StatusCode)
		}
	})
}

// TestHandleTopNodes exercises GET /api/nodes/top end-to-end. The metric=
// pagerank path returns 200 + a JSON array; an unknown metric yields 400.
func TestHandleTopNodes(t *testing.T) {
	store := buildFixture(t)
	srv := server.New(store, nil)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	t.Run("default_metric_is_pagerank", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/nodes/top?limit=5")
		if err != nil {
			t.Fatalf("GET /api/nodes/top: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status=%d body=%s", resp.StatusCode, body)
		}
		var out []any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		// Result may have fewer than 5 nodes on a tiny fixture; just verify
		// the route is wired and shape is an array.
		if len(out) > 5 {
			t.Errorf("limit=5 returned %d", len(out))
		}
	})

	t.Run("usage_metric_ok", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/nodes/top?metric=usage&limit=5")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status=%d, want 200", resp.StatusCode)
		}
	})

	t.Run("invalid_metric_400", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/nodes/top?metric=bogus")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status=%d, want 400", resp.StatusCode)
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

	// index.html is always tracked — either as the post-`make viewer` Next.js
	// page or the committed stub that explains how to build the viewer. Either
	// way, missing index.html means the embed is broken.
	indexPath := filepath.Join(dst, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		t.Errorf("index.html missing from dst: %v", err)
	}

	// _next/static/ is only present after `make viewer` has been run. On a
	// fresh clone (stub-only) it's gitignored so the directory is absent —
	// we log and skip the .js-count assertion in that case.
	staticDir := filepath.Join(dst, "_next", "static")
	if _, err := os.Stat(staticDir); err != nil {
		t.Logf("_next/static/ not present in embedded assets — stub-only mode (run `make viewer` to build the full viewer)")
		return
	}
	var jsCount int
	if err := filepath.WalkDir(staticDir, func(_ string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		if filepath.Ext(d.Name()) == ".js" {
			jsCount++
		}
		return nil
	}); err != nil {
		t.Errorf("WalkDir _next/static: %v", err)
	}
	if jsCount == 0 {
		t.Error("no .js files found under _next/static/ after CopyViewerAssetsTo")
	}
}
