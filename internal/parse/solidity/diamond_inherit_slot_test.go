package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W9 V18 — diamond inheritance storage slot offset audit.
//
// The diamond shape (D ← {B, C} ← A) is the canonical stress
// test for C3 linearization on storage layout. The same base
// (A) appears via two distinct paths; Solidity's storage rule
// says A's fields are laid out ONCE, not twice. A naive multi-
// base offset (sum of each base's slot count) would double-
// count A and place D's own field one slot too high.
//
// W9's resolve.go applies C3 via c3_linearization.go and computes
// the inheritance offset on the *deduplicated* MRO, so the
// expected layout is:
//
//	A.a → 0   (own-scope: A read in isolation)
//	B.b → 1   (B's own slot 0 + A's 1-slot offset)
//	C.c → 1   (C's own slot 0 + A's 1-slot offset)
//	D.d → 3   (D's own slot 0 + 3 slots from {A, B, C} deduped;
//	           NOT 4, which the naive doubled-A path would yield)
//
// V18 pins both axes:
//
//  1. **Per-field own-scope semantics** (matches V17): A.a, B.b,
//     C.c keep their own-scope slot because each base is read
//     in isolation. A regression that re-bases base contracts
//     to absolute-in-derived would flip these.
//
//  2. **MRO dedup on derived offset**: D.d = 3, not 4. A
//     regression that swaps c3_linearization for a naive sum
//     would silently move D.d up by one — and every diamond
//     contract in a cks-indexed corpus would report wrong
//     storage layout in lockstep.
//
// Storage layout regressions are particularly nasty: they don't
// cause compile errors, they don't fail tests on the contracts
// being audited, and they produce upgradeable-proxy collisions
// that surface as fund-loss bugs in production. The lockdown
// keeps the C3 invariant explicit.
func TestDiamondInheritance_SlotOffset(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/inheritance",
		"diamond_inherit_slot.sol")

	want := map[string]int{
		"DiamondBaseA.a":    0, // own-scope, base in isolation
		"DiamondLeftB.b":    1, // B own slot 0 + A(1) offset
		"DiamondRightC.c":   1, // C own slot 0 + A(1) offset
		"DiamondDerivedD.d": 3, // D own slot 0 + {A,B,C}(3) offset
		//                     ↑ NOT 4 — A is deduped via C3
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
