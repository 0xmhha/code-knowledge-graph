package mcphandlers

import (
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestFindCallers_InterfaceDispatchBridge verifies the interface-dispatch bridge:
// a caller that invokes an interface method must be found by find_callers on the
// CONCRETE implementation of that method.
//
// Fixture (testdata/resolve/coll1): UseHasher(h Hasher) calls h.Hash(); Thing
// implements Hasher with Hash(). The call is recorded as an `invokes` edge to the
// interface method coll1.Hasher.Hash, NOT to coll1.Thing.Hash — so a plain reverse
// walk from the concrete method misses UseHasher. reverseCallersUnion must recover
// it by bridging concrete→interface via the `implements` edge.
func TestFindCallers_InterfaceDispatchBridge(t *testing.T) {
	store := newFixtureStore(t)

	resolved, cands, ambiguous, ok := resolveSeed(store, "Thing.Hash", "")
	if ambiguous {
		t.Fatalf("seed Thing.Hash ambiguous: %v", cands)
	}
	if !ok {
		t.Skip("fixture has no coll1.Thing.Hash; skipping")
	}

	hasUseHasher := func(nodes []nodeQname) bool {
		for _, q := range nodes {
			if strings.HasSuffix(string(q), "UseHasher") {
				return true
			}
		}
		return false
	}

	// Negative control: a plain reverse walk from the concrete method does NOT
	// reach UseHasher (the call goes through the interface method).
	baseNodes, _, err := store.NeighborhoodByQname(resolved, 2, true, callEdgeTypes...)
	if err != nil {
		t.Fatalf("base NeighborhoodByQname: %v", err)
	}
	if hasUseHasher(qnames(baseNodes)) {
		t.Skip("fixture already links concrete method to the interface caller; bridge not exercised")
	}

	// With the bridge: UseHasher must now appear.
	nodes, _, err := reverseCallersUnion(store, resolved, 2)
	if err != nil {
		t.Fatalf("reverseCallersUnion: %v", err)
	}
	if !hasUseHasher(qnames(nodes)) {
		t.Fatalf("interface-dispatch bridge failed: find_callers(%s) did not include UseHasher; got %v", resolved, qnames(nodes))
	}
}

type nodeQname string

func qnames(ns []types.Node) []nodeQname {
	out := make([]nodeQname, 0, len(ns))
	for _, n := range ns {
		out = append(out, nodeQname(n.QualifiedName))
	}
	return out
}
