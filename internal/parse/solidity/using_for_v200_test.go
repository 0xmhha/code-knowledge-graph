package solidity_test

import (
	"testing"
)

// W-C W6 V2.0 — line-range scope-aware localVar lookup.
//
// V1.30 V0 introduced "first-decl wins" for shadowed locals, fixing
// the false-negative where inner-block shadows broke outer use sites.
// The trade-off: inner use sites where the inner shadow's type would
// be the correct resolution also resolved via the outer's type.
//
// V2.0 closes that trade-off by tracking each local's scope range
// (declStartLine, scopeEndLine) and resolving use-site dispatch via
// line containment + narrowest-scope-wins selection. Concretely:
//   - emitLocalVarBinding emits encoded `varName|typeName|scopeEndLine`
//     with pr.Line = declStartLine (already so).
//   - emitTryReturnsBinding follows the same encoding for try-returns /
//     catch-clause slots; the scope end is the success/catch block end.
//   - Pass 2 sweep builds localVarTypes as
//     funcID → varName → []localDecl{declLine, scopeEndLine, typeName}.
//   - lookupReceiverType gains a useSiteLine parameter; localVar lookup
//     finds decls where declLine <= useSiteLine <= scopeEndLine and
//     picks the one with the highest declLine (narrowest scope = inner).
//
// V2.0 carry-over (V2.1+):
//   - Full byte-range precision (instead of line). Edge cases of
//     >1 stmt per line resolve correctly. Practical Sol style makes
//     line-precision sufficient for V2.0 V0.
//   - Module/import handling additional patterns.
//   - Grammar-blocked items.

// TestUsingForV200_InnerShadowUse — outer uint256 x + inner bytes32 x
// + USE SITES IN BOTH SCOPES. V1.30 V0 misses inner use site (resolves
// to outer uint256, drops bytes32-dispatch). V2.0 fix: inner use →
// bytes32 → Other.tag; outer use → uint256 → SafeMath.add.
func TestUsingForV200_InnerShadowUse(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v200", "shadow_inner_use.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "C.f", target: "Other.tag"},
		{caller: "C.f", target: "SafeMath.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V2.0 inner shadow use) mismatch:\n got=%v\nwant=%v", got, want)
	}
}
