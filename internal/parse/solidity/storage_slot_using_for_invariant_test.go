package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W9 V9 — using-for storage-layout invariant audit. Sol §11.1
// states that `using` directives don't participate in storage
// layout. The slot index a contract assigns to its state vars
// must therefore be identical whether or not using directives
// surround the same declaration order. This test compares two
// twin contracts (WithUsing has three using shapes, NoUsing has
// none) and locks the invariant — a future change to the slot-
// indexing walker that accidentally consumes a using directive
// slot would surface here.
func TestStorageSlot_UsingForInvariant(t *testing.T) {
	withNodes, _ := parseResolveOneSol(t,
		"testdata/storage_slot_using_for_invariant", "with_using.sol")
	noNodes, _ := parseResolveOneSol(t,
		"testdata/storage_slot_using_for_invariant", "no_using.sol")

	collect := func(nodes []types.Node, contract string) map[string]int {
		out := map[string]int{}
		for _, n := range nodes {
			if n.Type != types.NodeField {
				continue
			}
			dot := -1
			for i := 0; i < len(n.QualifiedName); i++ {
				if n.QualifiedName[i] == '.' {
					dot = i
					break
				}
			}
			if dot <= 0 {
				continue
			}
			if n.QualifiedName[:dot] != contract {
				continue
			}
			out[n.QualifiedName[dot+1:]] = n.SlotIndex
		}
		return out
	}

	wantNames := []string{"a", "b", "c", "d", "e"}
	withSlots := collect(withNodes, "WithUsing")
	noSlots := collect(noNodes, "NoUsing")

	for _, name := range wantNames {
		w, hasW := withSlots[name]
		n, hasN := noSlots[name]
		if !hasW {
			t.Errorf("missing field WithUsing.%s in persisted graph", name)
			continue
		}
		if !hasN {
			t.Errorf("missing field NoUsing.%s in persisted graph", name)
			continue
		}
		if w != n {
			t.Errorf("using-for invariant violation on %q: WithUsing.SlotIndex=%d NoUsing.SlotIndex=%d", name, w, n)
		}
	}
}
