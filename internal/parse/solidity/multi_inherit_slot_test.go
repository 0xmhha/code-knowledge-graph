package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W9 V17 — multi-inheritance storage slot offset audit.
//
// Solidity's storage layout for `contract C is A, B` follows the
// reverse-MRO rule: base contracts lay out first, the derived
// contract's own fields go at the offset = sum of base slot counts.
// W9 V1 introduced single-inheritance offset; V17 pins the
// multi-base case so a regression in resolve.go's
// applyInheritanceStorageOffsets (or in c3_linearization.go's MRO
// computation) is caught before it ships.
//
// Slot model — confirmed by probe before this lock landed:
//
//   - Base contracts expose own-scope SlotIndex (the index INSIDE
//     that contract's own storage, no inheritance offset folded in).
//     MultiBaseA.a → 0, MultiBaseB.b → 0.
//
//   - Derived contracts fold the inheritance offset into their own
//     fields. MultiDerived.c → 2 (= MultiBaseA's 1 slot + MultiBaseB's 1
//     slot, both 1-slot uint256s).
//
// This split is deliberate: consumers that want absolute slots in
// the derived context can reconstruct them by walking the inheritance
// edges + summing slot counts per base; consumers that just want
// each contract's own layout (e.g. ABI dumpers, storage formatters)
// use the per-contract NodeField values directly. The lock preserves
// the split so a future refactor that re-bases everything to
// absolute-in-derived doesn't quietly change the cks-side meaning.
func TestMultiInheritance_SlotOffset(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/inheritance",
		"multi_inherit_slot.sol")

	want := map[string]int{
		"MultiBaseA.a": 0, // base own-scope slot
		"MultiBaseB.b": 0, // base own-scope slot (NOT 1 — base
		// is read in isolation, not folded into the derived layout)
		"MultiDerived.c": 2, // derived own field, offset by A(1) + B(1)
	}
	got := map[string]int{}
	for _, n := range nodes {
		if n.Type != types.NodeField {
			continue
		}
		if _, ok := want[n.QualifiedName]; !ok {
			continue
		}
		got[n.QualifiedName] = n.SlotIndex
	}
	for qn, w := range want {
		g, present := got[qn]
		if !present {
			t.Errorf("missing field %q", qn)
			continue
		}
		if g != w {
			t.Errorf("%s SlotIndex: got %d, want %d", qn, g, w)
		}
	}
}
