package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W9 V20 — deeper-diamond inheritance MRO offset audit.
//
// V18 covered the 3-node diamond (D ← {B, C} ← A): one shared
// base reached via two direct paths. V20 stresses C3
// linearization on a *deeper* chain where the shared base is
// two levels up:
//
//	 A
//	 |
//	 B          (B inherits A)
//	/ \
//
// C   D        (C inherits B; D inherits B)
//
//	\ /
//	 E          (E is C, D)
//
// MRO(E) = [E, C, D, B, A]  →  storage walk = [A, B, D, C, E].
//
// The fixture also clarifies the slot model that V17 / V18 only
// hinted at: ckg reports SlotIndex on a NodeField with the
// inheritance offset *folded in* for any contract that has
// inheritance, regardless of depth. The own-scope-only case is
// reserved for *root* base contracts (no inheritance). The
// expected values below crystallise that rule:
//
//	DeepBaseA.a    → 0   (root: own scope, slot 0 literal)
//	DeepMidB.b     → 1   (derived: B own slot 0 + A offset 1)
//	DeepLeftC.c    → 2   (derived: C own slot 0 + (A+B) = 2)
//	DeepRightD.d   → 2   (derived: D own slot 0 + (A+B) = 2)
//	DeepBottomE.e  → 4   (derived: E own slot 0 + dedup
//	                      {A,B,C,D} = 4; NOT 5, which the naive
//	                      doubled-B path would produce)
//
// The slot value for C and D is *the same* (both 2) because each
// contract reports the slot of its own first field in its own
// linearised layout — and both C and D linearise identically on
// top of B. Consumers that need "where does C.c live inside E's
// layout?" reconstruct it by walking the inheritance edges plus
// the slot-count side table; V20 does NOT change that contract,
// only pins it.
//
// Why deeper diamonds matter:
//   - V18 only proved direct multi-base dedup. A regression that
//     dropped *transitive* dedup (e.g. someone simplified MRO to
//     immediate parents only) would pass V18 and fail V20.
//   - The order-vs-set distinction in C3 only shows up at depth
//     >= 2 — for a flat diamond, set semantics produce the right
//     answer accidentally. V20 forces the order through D-before-C
//     in the storage walk.
func TestDeeperDiamond_SlotOffset(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/inheritance",
		"deeper_diamond_slot.sol")

	want := map[string]int{
		"DeepBaseA.a":   0, // root base, own scope
		"DeepMidB.b":    1, // B own 0 + A(1) offset
		"DeepLeftC.c":   2, // C own 0 + (A+B)(2) offset
		"DeepRightD.d":  2, // D own 0 + (A+B)(2) offset
		"DeepBottomE.e": 4, // E own 0 + dedup{A,B,C,D}(4); not 5
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
