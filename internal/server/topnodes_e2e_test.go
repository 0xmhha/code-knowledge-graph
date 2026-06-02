package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/internal/server"
)

// TestTopNodesAgainstSelfGraph is a developer-only sanity check that opens
// /tmp/ckg-self/graph.db (built by `ckg build --src=. --out=/tmp/ckg-self`)
// and prints the top-10 nodes by pagerank. Skipped automatically when the
// graph isn't present so CI is unaffected.
//
// Skipped automatically when /tmp/ckg-self/graph.db is absent so a fresh
// clone never sees a spurious failure. Set CKG_E2E_SKIP_SELFGRAPH=1 to
// force-skip even when the file exists (useful when a stale graph is
// hanging around on a developer machine).
func TestTopNodesAgainstSelfGraph(t *testing.T) {
	if os.Getenv("CKG_E2E_SKIP_SELFGRAPH") == "1" {
		t.Skip("CKG_E2E_SKIP_SELFGRAPH=1; opted out of self-graph check")
	}
	dbPath := "/tmp/ckg-self/graph.db"
	if _, err := os.Stat(dbPath); err != nil {
		t.Skipf("no self-graph at %s: %v", dbPath, err)
	}
	store, err := persist.OpenReadOnly(filepath.Clean(dbPath))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = store.Close() }()
	srv := server.New(store, nil)
	ts := httptest.NewServer(srv)
	defer func() { ts.Close() }()

	resp, err := http.Get(ts.URL + "/api/nodes/top?metric=pagerank&limit=10")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var arr []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
		t.Fatalf("decode: %v", err)
	}

	var prev = 1e18
	for i, n := range arr {
		pr, _ := n["pagerank"].(float64)
		fmt.Printf("#%d  pr=%.6f  type=%v  name=%v  qname=%v\n",
			i+1, pr, n["type"], n["name"], n["qualified_name"])
		if pr > prev+1e-9 {
			t.Errorf("pagerank not descending at index %d: prev=%.9f cur=%.9f", i, prev, pr)
		}
		prev = pr
	}
	if len(arr) == 0 {
		t.Fatal("expected at least one node")
	}
}
