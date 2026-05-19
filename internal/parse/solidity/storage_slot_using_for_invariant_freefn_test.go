package solidity_test

import (
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W9 V10 — free-function using-form storage-layout invariant.
// Parallels the V9 operator-form audit for Sol 0.8.13+
// `using {f} for T;` (no operator). The shape goes through a
// different misparse path in vendored tree-sitter-solidity, so the
// V9 fix in runStateVarDecl only covers operator-form; this audit
// catches a future regression in the free-function path. The
// SlotIndex on every state variable must match the equivalent
// no-using contract.
func TestStorageSlot_UsingForInvariantFreeFn(t *testing.T) {
	withNodes, _ := parseResolveOneSol(t,
		"testdata/storage_slot_using_for_invariant_freefn", "with_using.sol")
	noNodes, _ := parseResolveOneSol(t,
		"testdata/storage_slot_using_for_invariant_freefn", "no_using.sol")

	collect := func(nodes []types.Node, contract string) map[string]int {
		out := map[string]int{}
		for _, n := range nodes {
			if n.Type != types.NodeField {
				continue
			}
			dot := strings.IndexByte(n.QualifiedName, '.')
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
	withSlots := collect(withNodes, "WithUsingFreeFn")
	noSlots := collect(noNodes, "NoUsingFreeFn")

	for _, name := range wantNames {
		w, hasW := withSlots[name]
		n, hasN := noSlots[name]
		if !hasW {
			t.Errorf("missing WithUsingFreeFn.%s", name)
			continue
		}
		if !hasN {
			t.Errorf("missing NoUsingFreeFn.%s", name)
			continue
		}
		if w != n {
			t.Errorf("free-function using-for invariant violation on %q: WithUsingFreeFn.SlotIndex=%d NoUsingFreeFn.SlotIndex=%d", name, w, n)
		}
	}
}
