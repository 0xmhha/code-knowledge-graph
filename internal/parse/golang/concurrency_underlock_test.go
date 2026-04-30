package golang_test

import (
	"strings"
	"testing"

	gop "github.com/0xmhha/code-knowledge-graph/internal/parse/golang"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestConcurrency_AccessedUnderLock_Basic asserts G8 (B1 Phase 4) emits at
// least one accessed_under_lock edge from a Field node to a Mutex node for
// the existing concurrency fixture. The Counter struct has a `count`
// field protected by `mu`; the Inc/Get methods both lock and touch it,
// so the V0 simplification (any Lock in function ⇒ all field accesses
// counted) yields edges Counter.count → Counter.mu.
func TestConcurrency_AccessedUnderLock_Basic(t *testing.T) {
	root := "testdata/concurrency"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}

	underLock := edgesByType(g.Edges, types.EdgeAccessedUnderLock)
	if len(underLock) == 0 {
		t.Fatal("expected >=1 accessed_under_lock edge; got 0")
	}

	// Pinpoint at least one edge: Counter.count (Field) → Counter.mu (Mutex).
	var counterCountID, counterMuID string
	for _, n := range g.Nodes {
		switch {
		case n.Type == types.NodeField && strings.HasSuffix(n.QualifiedName, "Counter.count"):
			counterCountID = n.ID
		case n.Type == types.NodeMutex && strings.HasSuffix(n.QualifiedName, "Counter.mu#mutex"):
			counterMuID = n.ID
		}
	}
	if counterCountID == "" || counterMuID == "" {
		t.Fatalf("missing fixture nodes: count=%q mu=%q", counterCountID, counterMuID)
	}
	found := false
	for _, e := range underLock {
		if e.Src == counterCountID && e.Dst == counterMuID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected accessed_under_lock(Counter.count -> Counter.mu); got %d edges, none matched", len(underLock))
	}

	// FakeMutex.UseFake locks a user-defined Lock(); no Mutex node maps to it,
	// so no accessed_under_lock edges should target FakeMutex's nonexistent
	// node. (Negative assertion is implicit — none of the iterated edges
	// reference a FakeMutex node ID, because none was emitted.)
	for _, e := range underLock {
		dst := findNodeByID(g.Nodes, e.Dst)
		if dst != nil && strings.Contains(dst.QualifiedName, "FakeMutex") {
			t.Errorf("accessed_under_lock to FakeMutex.* — false-positive guard failed: %s", dst.QualifiedName)
		}
	}
}
