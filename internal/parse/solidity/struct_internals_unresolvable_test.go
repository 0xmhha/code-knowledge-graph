package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W9 V28 — struct-internal reference-type slot fallback characterization.
//
// This is a *characterization lock*: it pins the walker's current
// behaviour as test data so future changes are visible diffs
// instead of silent flips. The behaviour pinned here is known to
// diverge from V5's stated intent (struct_size.go); the divergence
// itself is the value the test captures.
//
// What the probe (2026-05-21) revealed:
//
//   - V5 (struct_size.go) comments document mapping / dynamic-bytes
//     / dynamic-array fields inside structs as "use the conservative
//     32-byte slot" or "use the full-slot mapping advance" — i.e.
//     each such member should count as one full slot when
//     computing the struct's total byte footprint.
//
//   - tryComputeStructBytes actually classifies members through
//     three helpers: typeNameIsMapping, solFixedArrayBytes (fixed
//     `T[N]` only), and solValueTypeSize (primitives + bytes1..32
//     only). Dynamic `bytes`, dynamic `string`, and dynamic `T[]`
//     hit none, the helper returns (0, false), and the struct
//     stays out of v.structSizes across the fixed-point pass.
//
//   - State-var declarations whose type is unresolved in
//     structSizes fall back to the conservative 1-slot path.
//
//   - A second observation surfaces from this fixture but not from
//     V11 (struct_deeply_nested.sol): when several contracts in
//     the same file each declare a struct with the same identifier
//     (here, `S`), even the all-primitive struct that *would* be
//     resolvable in isolation lands on the same 1-slot fallback
//     in this fixture. PrimitiveOnly.tail = 2, not 4. Whether the
//     root cause is structSizes name-collision across contracts,
//     state-var lookup using unqualified vs qualified names, or
//     some interaction with the fixed-point loop is outside V28's
//     scope; V28 only records that all four shapes land on the
//     same fallback today.
//
// V28's pinned expectations: all four contracts produce
// head=0, inner=1, tail=2. A walker change that flips ANY of these
// cells is a behaviour change worth reviewing:
//
//   - If only the three failure cells (WithMap/WithBytes/WithDynArray)
//     flip, the V5 comment intent has been resolved and the
//     all-primitive baseline is still fallback-bound — the test
//     must capture which cells moved and the WALKER_SYMMETRY drift
//     catalogue row 6 entry updates accordingly.
//
//   - If the PrimitiveOnly baseline ALSO flips, struct sizing
//     across multi-contract files now works and *that* is the
//     newsworthy change; the WALKER_SYMMETRY drift catalogue
//     should note multi-contract resolution alongside the
//     reference-type member handling.
//
// Cross-flip protocol: this test pinning the entire 4x3 grid
// forces a future walker fix author to face the whole picture in
// one diff, not just the cell they intended to touch.
func TestW9V28_StructInternalsFallback_Characterization(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/storage_slot_packing",
		"struct_internals_unresolvable.sol")

	// All four shapes currently land on the same fallback:
	// head at slot 0, inner takes 1 slot, tail at slot 2.
	want := map[string]int{
		"PrimitiveOnly.head":  0,
		"PrimitiveOnly.inner": 1,
		"PrimitiveOnly.tail":  2,
		"WithMap.head":        0,
		"WithMap.inner":       1,
		"WithMap.tail":        2,
		"WithBytes.head":      0,
		"WithBytes.inner":     1,
		"WithBytes.tail":      2,
		"WithDynArray.head":   0,
		"WithDynArray.inner":  1,
		"WithDynArray.tail":   2,
	}
	got := map[string]int{}
	for _, n := range nodes {
		if n.Type != types.NodeField {
			continue
		}
		if _, ok := want[n.QualifiedName]; ok {
			got[n.QualifiedName] = n.SlotIndex
		}
	}
	for qn, w := range want {
		g, present := got[qn]
		if !present {
			t.Errorf("missing field %q", qn)
			continue
		}
		if g != w {
			t.Errorf("%s SlotIndex: got %d, want %d\n  (V28 characterization flipped — read the test header to identify which subset of cells moved and update WALKER_SYMMETRY drift row 6 accordingly)",
				qn, g, w)
		}
	}
}
