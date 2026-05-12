package solidity_test

import (
	"testing"
)

// W-C W6 V1.30 — block-scoped shadowing (V0 first-decl-wins).
//
// Pre-V1.30 the Pass 2 sweep of dispatchKindUsingForLocalVar PendingRefs
// wrote into localVarTypes with map overwrite semantics — the LAST
// emit per (funcID, varName) won. With block-scoped shadowing where
// an inner block redeclares an outer variable, this meant inner
// type bled out into outer use sites — typically dropping the
// dispatch (false-negative) when inner type has no using-for binding.
//
// V1.30 V0 changes the sweep to "first-decl wins": only set the
// localVarTypes entry when no entry exists yet for the same key. Tree-
// sitter source-order traversal means the outermost (earliest) decl is
// emitted first, so it wins. Trade-off: inner-block use sites where
// the inner shadow's type would be the correct resolution now also
// resolve via the outer's type — V2+ refactor with byte-range scope-
// aware lookup is needed for full correctness.
//
// V1.30 V0 carry-over (V2.0+):
//   - Full byte-range scope-aware lookup (per-block localVarTypes
//     stack with parent fallback, or PendingRef byte-tagged entries
//     + scope-end byte tracking).
//   - Module/import handling additional patterns.
//   - Grammar-blocked items.

// TestUsingForV130_ShadowOuterWins — outer x (uint256, SafeMath-bound)
// shadowed by inner-block x (bytes32, unbound). Outer use site
// `x.add(1)` in the return statement must resolve via the outer type.
// Pre-V1.30 the inner shadow overwrites and the outer use drops →
// RED. V1.30 V0 fix flips precedence to outer.
func TestUsingForV130_ShadowOuterWins(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v130", "shadow_outer_wins.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "C.f", target: "SafeMath.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V1.30 outer shadow wins) mismatch:\n got=%v\nwant=%v", got, want)
	}
}
