package solidity_test

import (
	"testing"
)

// W-C W6 V2.15 — same-line shadow disambiguation via byte-offset
// scope containment.
//
// V2.0 introduced line-range scope-aware localVar lookup, fixing the
// V1.30 trade-off for multi-line shadowing. The carry-over (V2.1+)
// flagged ">1 stmt per line edge cases" — when an outer decl, an
// inner-block shadow decl, and use sites for both all sit on the
// same physical line, V2.0's line-only filter accepts both decls for
// either use site, and the tiebreak (`declLine > bestDeclLine`, strict
// `>`) keeps the first-appended (outer). Inner use sites lose to the
// outer scope — a return of the V1.30 V0 false-negative, scoped to
// same-line code.
//
// V2.15 closes the gap by tracking byte-offset scope ranges
// (declStartByte, scopeEndByte) alongside line ranges, threading
// useSiteByte through the lookup chain, and tie-breaking on max
// declStartByte (rightmost = innermost source position).
//
// Fixture: V2.0's shadow_inner_use.sol with the function body
// compressed to a single line.

// TestUsingForV2150_SameLineShadow — outer `uint256 x` + inner-block
// `bytes32 x` + both use sites all on one line. Asserts both edges
// resolve correctly (inner → Other.tag, outer → SafeMath.add). Pre-
// V2.15 (V2.0 line-only): inner use drops because the outer decl
// wins the tiebreak.
func TestUsingForV2150_SameLineShadow(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v2150", "shadow_same_line.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "C.f", target: "Other.tag"},
		{caller: "C.f", target: "SafeMath.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V2.15 same-line shadow) mismatch:\n got=%v\nwant=%v", got, want)
	}
}
