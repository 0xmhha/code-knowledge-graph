package concurrency_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/concurrency"
	"github.com/0xmhha/code-knowledge-graph/pkg/store"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestAnalyze_GoStablenetSmoke validates pkg/concurrency.Analyze against a REAL
// go-stablenet graph (R1' M2.a). Beyond "non-empty", it cross-checks Analyze's
// edge set against an INDEPENDENT read of the seed's concurrency edges from the
// store — so a regression in the traversal/filter is caught, not just emptiness.
//
// Opt-in (slow / requires a built graph). Build once:
//
//	ckg build --src <go-stablenet> --out /tmp/gsn-graph --no-cache
//	CKG_GSN_GRAPH=/tmp/gsn-graph go test ./pkg/concurrency/ -run GoStablenetSmoke -v
//
// CKG_GSN_GRAPH accepts a graph.db file OR a directory containing one. Skipped
// when unset so normal CI stays fast.
func TestAnalyze_GoStablenetSmoke(t *testing.T) {
	dbPath := os.Getenv("CKG_GSN_GRAPH")
	if dbPath == "" {
		t.Skip("set CKG_GSN_GRAPH to a graph.db file (or a dir containing one) to run the go-stablenet smoke")
	}
	if info, statErr := os.Stat(dbPath); statErr == nil && info.IsDir() {
		dbPath = filepath.Join(dbPath, "graph.db")
	}
	r, err := store.OpenReadOnly(dbPath)
	if err != nil {
		t.Fatalf("open graph %q: %v", dbPath, err)
	}
	defer func() { _ = r.Close() }()

	// Concurrency edges must exist on real go-stablenet code.
	locks, err := r.QueryEdgesByType(string(types.EdgeAcquiresLock))
	if err != nil {
		t.Fatalf("QueryEdgesByType(acquires_lock): %v", err)
	}
	if len(locks) == 0 {
		t.Fatal("expected acquires_lock edges in the go-stablenet graph, got 0")
	}
	t.Logf("acquires_lock edges: %d", len(locks))

	concSet := map[string]bool{}
	for _, et := range concurrency.ConcurrencyEdgeTypes {
		concSet[et] = true
	}

	// Find a lock-acquiring function with a non-empty blast radius.
	var (
		seedID, seedQ string
		res           concurrency.Result
		found         bool
		tried         int
	)
	for _, e := range locks {
		if tried >= 25 {
			break
		}
		ns, nerr := r.NodesByIDs([]string{e.Src})
		if nerr != nil || len(ns) == 0 || ns[0].QualifiedName == "" {
			continue
		}
		tried++
		rr, aerr := concurrency.Analyze(r, ns[0].QualifiedName, concurrency.Options{Depth: 1})
		if aerr != nil {
			t.Fatalf("Analyze(%q): %v", ns[0].QualifiedName, aerr)
		}
		if !rr.NotFound && len(rr.Modules) > 0 {
			seedID, seedQ, res, found = e.Src, ns[0].QualifiedName, rr, true
			break
		}
	}
	if !found {
		t.Fatalf("no lock-acquiring seed yielded a non-empty concurrency blast radius (tried %d)", tried)
	}

	// (A) GROUND-TRUTH CROSS-CHECK: every forward concurrency edge of the seed,
	// read INDEPENDENTLY from the store, must appear in Analyze's edge set.
	aset := map[string]bool{}
	for _, e := range res.Edges {
		aset[fmt.Sprintf("%s|%s|%s|%d", e[0].(string), e[1].(string), e[2].(string), e[3].(int))] = true
	}
	raw, err := r.QueryEdgesForNodes([]string{seedID})
	if err != nil {
		t.Fatalf("QueryEdgesForNodes: %v", err)
	}
	gtForward := 0
	for _, e := range raw {
		if e.Src != seedID || !concSet[string(e.Type)] {
			continue
		}
		gtForward++
		k := fmt.Sprintf("%s|%s|%s|%d", e.Src, e.Dst, string(e.Type), e.Line)
		if !aset[k] {
			t.Errorf("ground-truth forward concurrency edge missing from Analyze output: %s", k)
		}
	}
	if gtForward == 0 {
		t.Fatalf("seed %q had no forward concurrency edges in independent ground truth", seedQ)
	}
	t.Logf("seed=%q modules=%d analyzeEdges=%d groundTruthForward=%d (all present)",
		seedQ, len(res.Modules), len(res.Edges), gtForward)

	// (B) A lock-acquiring function's blast radius must include a Mutex.
	var sawMutex bool
	for _, m := range res.Modules {
		if m.Type == types.NodeMutex {
			sawMutex = true
			break
		}
	}
	if !sawMutex {
		t.Errorf("expected a Mutex in the blast radius of lock-acquiring %q, got %+v", seedQ, res.Modules)
	}

	// (C) Source blob must be populated (G3).
	if b, gerr := r.GetBlob(seedID); gerr != nil || len(b) == 0 {
		t.Errorf("expected non-empty source blob for seed %q (G3): err=%v len=%d", seedQ, gerr, len(b))
	}
}
