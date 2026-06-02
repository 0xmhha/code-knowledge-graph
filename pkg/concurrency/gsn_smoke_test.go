package concurrency_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/concurrency"
	"github.com/0xmhha/code-knowledge-graph/pkg/store"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestAnalyze_GoStablenetSmoke validates pkg/concurrency.Analyze against a REAL
// go-stablenet graph (R1' M2.a: ConcurrencyImpact returns non-empty on a real
// concurrency symbol; G3: source blobs are populated).
//
// Opt-in (slow / requires a built graph). Build once:
//
//	ckg build --src <go-stablenet> --out /tmp/gsn-graph --no-cache
//	CKG_GSN_GRAPH=/tmp/gsn-graph go test ./pkg/concurrency/ -run GoStablenetSmoke -v
//
// Skipped when CKG_GSN_GRAPH is unset so normal CI stays fast.
func TestAnalyze_GoStablenetSmoke(t *testing.T) {
	dbPath := os.Getenv("CKG_GSN_GRAPH")
	if dbPath == "" {
		t.Skip("set CKG_GSN_GRAPH to a graph.db file (or a dir containing one) to run the go-stablenet smoke")
	}
	// Flexible: accept either a direct graph.db file path or a directory
	// containing graph.db (the ckg --out convention). The db may live anywhere.
	if info, statErr := os.Stat(dbPath); statErr == nil && info.IsDir() {
		dbPath = filepath.Join(dbPath, "graph.db")
	}
	r, err := store.OpenReadOnly(dbPath)
	if err != nil {
		t.Fatalf("open graph %q: %v", dbPath, err)
	}
	defer r.Close()

	// (1) Concurrency edges must exist on real go-stablenet code.
	locks, err := r.QueryEdgesByType(string(types.EdgeAcquiresLock))
	if err != nil {
		t.Fatalf("QueryEdgesByType(acquires_lock): %v", err)
	}
	if len(locks) == 0 {
		t.Fatal("expected acquires_lock edges in the go-stablenet graph, got 0")
	}
	t.Logf("acquires_lock edges: %d", len(locks))

	// (2) A lock-acquiring function must yield a non-empty concurrency blast
	// radius (M2.a). Try several seeds to stay robust against qname-resolution
	// quirks on any single node. (3) Its source blob must be populated (G3).
	var (
		okSeed    string
		okModules int
		okEdges   int
		blobOK    bool
		anyMutex  bool
		tried     int
	)
	for _, e := range locks {
		if tried >= 25 {
			break
		}
		ns, err := r.NodesByIDs([]string{e.Src})
		if err != nil || len(ns) == 0 || ns[0].QualifiedName == "" {
			continue
		}
		tried++
		seed := ns[0].QualifiedName
		res, err := concurrency.Analyze(r, seed, concurrency.Options{Depth: 3})
		if err != nil {
			t.Fatalf("Analyze(%q): %v", seed, err)
		}
		if res.NotFound || len(res.Modules) == 0 {
			continue
		}
		for _, m := range res.Modules {
			if m.Type == types.NodeMutex {
				anyMutex = true
			}
		}
		if okSeed == "" {
			okSeed, okModules, okEdges = seed, len(res.Modules), len(res.Edges)
			if b, gerr := r.GetBlob(e.Src); gerr == nil && len(b) > 0 {
				blobOK = true
			}
		}
		if anyMutex {
			break
		}
	}

	if okSeed == "" {
		t.Fatalf("no lock-acquiring seed yielded a non-empty concurrency blast radius (tried %d)", tried)
	}
	t.Logf("seed=%q modules=%d edges=%d sawMutex=%v blobOK=%v", okSeed, okModules, okEdges, anyMutex, blobOK)

	if !blobOK {
		t.Errorf("expected non-empty source blob for a lock-acquiring seed (G3)")
	}
	if !anyMutex {
		t.Errorf("expected at least one Mutex in the blast radius of a lock-acquiring function across %d seeds", tried)
	}
}
