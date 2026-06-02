package buildpipe_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestLockPropagation_DefaultOff_NoEmit is the §7.0 Go regression guard at
// test level: --lock-propagation defaults to false, so building the W-A
// fixture without the flag must produce the same accessed_under_lock count
// as the existing B1 Phase 4 intra-function pass. Catches accidental
// "default-on" drift before it reaches CI.
func TestLockPropagation_DefaultOff_NoEmit(t *testing.T) {
	off := buildAndQueryUnderLock(t, "testdata/lock_propagation", false)
	on := buildAndQueryUnderLock(t, "testdata/lock_propagation", true)
	if len(on) <= len(off) {
		t.Fatalf("flag ON must emit more edges than OFF; got off=%d on=%d", len(off), len(on))
	}
	// The OFF count must match the intra-function B1 pass exactly — every
	// edge in OFF must also exist in ON (set inclusion). Catches the case
	// where ON inadvertently overwrites or drops an OFF edge.
	onSet := pairSet(on)
	for _, e := range off {
		k := edgeKey(e)
		if _, ok := onSet[k]; !ok {
			t.Errorf("OFF edge missing from ON set: src=%s dst=%s", e.Src, e.Dst)
		}
	}
}

// TestLockPropagation_SingleHop verifies the canonical W-A positive case:
// Apply locks mu, calls touch(); touch reads/writes value. After
// --lock-propagation the field's accessed_under_lock edge appears, and the
// confidence is INFERRED (W-A §5.0 Q2 — all cross-fn emits are INFERRED).
func TestLockPropagation_SingleHop(t *testing.T) {
	edges, nodes := buildAndQueryFull(t, "testdata/lock_propagation", true)
	valueID := findNodeIDBySuffix(nodes, "SingleHop.value")
	muID := findNodeIDBySuffix(nodes, "SingleHop.mu#mutex")
	if valueID == "" || muID == "" {
		t.Fatalf("fixture nodes missing: value=%q mu=%q", valueID, muID)
	}
	var found *types.Edge
	for i, e := range edges {
		if e.Src == valueID && e.Dst == muID && e.Type == types.EdgeAccessedUnderLock {
			found = &edges[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected accessed_under_lock(SingleHop.value -> SingleHop.mu); got none")
	}
	if found.Confidence != types.ConfInferred {
		t.Errorf("expected INFERRED confidence for cross-fn emit; got %q", found.Confidence)
	}
}

// TestLockPropagation_DeepChain verifies DFS reaches a depth-5 helper chain.
// Enter -> level0 -> level1 -> level2 -> level3 -> terminal; only terminal
// touches DeepChain.value. Without DFS the edge would be missing.
//
// Also pins the cross-fn confidence policy (W-A §5.0 Q2): all propagated
// edges must be INFERRED regardless of DFS depth — the suspicion-grade
// label that signals "could not have been derived from a single function
// body" doesn't degrade further as the chain lengthens.
func TestLockPropagation_DeepChain(t *testing.T) {
	edges, nodes := buildAndQueryFull(t, "testdata/lock_propagation", true)
	valueID := findNodeIDBySuffix(nodes, "DeepChain.value")
	muID := findNodeIDBySuffix(nodes, "DeepChain.mu#mutex")
	if valueID == "" || muID == "" {
		t.Fatalf("fixture nodes missing: value=%q mu=%q", valueID, muID)
	}
	found := findEdge(edges, valueID, muID, types.EdgeAccessedUnderLock)
	if found == nil {
		t.Fatalf("expected accessed_under_lock(DeepChain.value -> DeepChain.mu) via DFS depth>=5")
	}
	if found.Confidence != types.ConfInferred {
		t.Errorf("DeepChain cross-fn emit confidence=%q, want INFERRED (W-A §5.0 Q2)", found.Confidence)
	}
}

// TestLockPropagation_Cycle verifies the visited set defends against
// infinite recursion in the call graph. cycleA <-> cycleB cycle: the build
// must terminate AND at least one accessed_under_lock edge for Cycle.value
// must be emitted (cycleA touches it).
//
// Confidence assertion parallels DeepChain — a cycle-pruned DFS doesn't
// change the cross-fn label. Without this assertion a future regression
// could silently downgrade the cycle path's edge to AMBIGUOUS / EXTRACTED
// while still passing the existence check.
func TestLockPropagation_Cycle(t *testing.T) {
	edges, nodes := buildAndQueryFull(t, "testdata/lock_propagation", true)
	valueID := findNodeIDBySuffix(nodes, "Cycle.value")
	muID := findNodeIDBySuffix(nodes, "Cycle.mu#mutex")
	if valueID == "" || muID == "" {
		t.Fatalf("fixture nodes missing: value=%q mu=%q", valueID, muID)
	}
	found := findEdge(edges, valueID, muID, types.EdgeAccessedUnderLock)
	if found == nil {
		t.Fatalf("expected accessed_under_lock(Cycle.value -> Cycle.mu) — cycleA touches value")
	}
	if found.Confidence != types.ConfInferred {
		t.Errorf("Cycle cross-fn emit confidence=%q, want INFERRED (W-A §5.0 Q2)", found.Confidence)
	}
}

// TestLockPropagation_StdlibSkip verifies §3.3 noise control: callees
// outside the build (fmt.Println) must not surface as edge endpoints. We
// can't assert "no edge to fmt.Println" directly (it has no node), but we
// can verify that the edge count for StdlibSkip-related accessed_under_lock
// edges is exactly the intra-fn count (1, for the mu field itself).
func TestLockPropagation_StdlibSkip(t *testing.T) {
	edges, nodes := buildAndQueryFull(t, "testdata/lock_propagation", true)
	muID := findNodeIDBySuffix(nodes, "StdlibSkip.mu#mutex")
	if muID == "" {
		t.Fatalf("fixture node missing: StdlibSkip.mu#mutex")
	}
	// Count accessed_under_lock edges targeting StdlibSkip.mu.
	count := 0
	for _, e := range edges {
		if e.Type == types.EdgeAccessedUnderLock && e.Dst == muID {
			count++
		}
	}
	// Only the mu field itself touches (s.mu.Lock() references s.mu); no
	// propagation edges from inside fmt.Println (which isn't in the graph).
	if count != 1 {
		t.Errorf("expected exactly 1 accessed_under_lock to StdlibSkip.mu (intra-fn only); got %d", count)
	}
}

// TestLockPropagation_NamedGoroutine verifies the P2 #8 W-A fix: a
// `go gh.touchAsync()` call from inside a lock-holding chain now
// surfaces an accessed_under_lock edge on the goroutine target's
// field accesses. Pre-fix the Go parser emitted only a `spawns` edge
// for named-function goroutines, so the propagator's calls/invokes
// DFS skipped the target body entirely; the GoroutineHolder fixture
// documented the gap as a known limitation. statements.go GoStmt
// now queues a PendingRef alongside spawns so Pass 2 Resolve
// materialises the calls edge, and the propagator reaches the field.
//
// Q4 ("Goroutine body INFERRED") rides the existing uniform cross-
// fn INFERRED label — confidence stays consistent with the other
// W-A propagation paths.
func TestLockPropagation_NamedGoroutine(t *testing.T) {
	edges, nodes := buildAndQueryFull(t, "testdata/lock_propagation", true)
	valueID := findNodeIDBySuffix(nodes, "GoroutineHolder.value")
	muID := findNodeIDBySuffix(nodes, "GoroutineHolder.mu#mutex")
	if valueID == "" || muID == "" {
		t.Fatalf("fixture nodes missing: value=%q mu=%q", valueID, muID)
	}
	found := findEdge(edges, valueID, muID, types.EdgeAccessedUnderLock)
	if found == nil {
		t.Fatalf("expected accessed_under_lock(GoroutineHolder.value -> GoroutineHolder.mu) via named-goroutine path")
	}
	if found.Confidence != types.ConfInferred {
		t.Errorf("named-goroutine cross-fn emit confidence=%q, want INFERRED (W-A §5.0 Q4)", found.Confidence)
	}
}

// TestLockPropagation_NoLockNoEdge verifies the negative guard: when the
// caller doesn't acquire any lock, the callee's field accesses produce
// zero accessed_under_lock edges even via propagation.
func TestLockPropagation_NoLockNoEdge(t *testing.T) {
	edges, nodes := buildAndQueryFull(t, "testdata/lock_propagation", true)
	valueID := findNodeIDBySuffix(nodes, "NoLockNoEdge.value")
	if valueID == "" {
		t.Fatalf("fixture node missing: NoLockNoEdge.value")
	}
	for _, e := range edges {
		if e.Type == types.EdgeAccessedUnderLock && e.Src == valueID {
			t.Errorf("unexpected accessed_under_lock for unlocked caller: src=%s dst=%s", e.Src, e.Dst)
		}
	}
}

// helpers --------------------------------------------------------------

func buildAndQueryFull(t *testing.T, src string, lockProp bool) ([]types.Edge, []types.Node) {
	t.Helper()
	out := t.TempDir()
	_, err := buildpipe.Run(buildpipe.Options{
		SrcRoot:         src,
		OutDir:          out,
		Languages:       []string{"go"},
		CKGVersion:      "test",
		NoCache:         true,
		LockPropagation: lockProp,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	store, err := persist.OpenReadOnly(filepath.Join(out, "graph.db"))
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	defer func() { _ = store.Close() }()
	edges, err := store.QueryEdgesByType(string(types.EdgeAccessedUnderLock))
	if err != nil {
		t.Fatalf("QueryEdgesByType: %v", err)
	}
	nodes, err := store.AllNodes()
	if err != nil {
		t.Fatalf("AllNodes: %v", err)
	}
	return edges, nodes
}

func buildAndQueryUnderLock(t *testing.T, src string, lockProp bool) []types.Edge {
	t.Helper()
	edges, _ := buildAndQueryFull(t, src, lockProp)
	return edges
}

func findNodeIDBySuffix(nodes []types.Node, suffix string) string {
	for _, n := range nodes {
		if strings.HasSuffix(n.QualifiedName, suffix) {
			return n.ID
		}
	}
	return ""
}

func findEdge(edges []types.Edge, src, dst string, t types.EdgeType) *types.Edge {
	for i, e := range edges {
		if e.Src == src && e.Dst == dst && e.Type == t {
			return &edges[i]
		}
	}
	return nil
}

type lpKey struct{ src, dst string }

func edgeKey(e types.Edge) lpKey { return lpKey{src: e.Src, dst: e.Dst} }

func pairSet(edges []types.Edge) map[lpKey]struct{} {
	out := make(map[lpKey]struct{}, len(edges))
	for _, e := range edges {
		out[edgeKey(e)] = struct{}{}
	}
	return out
}
