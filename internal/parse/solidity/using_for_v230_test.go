package solidity_test

import (
	"testing"
)

// W-C W6 V2.3 — library body using-for regression guard.
//
// V0/V1.0 queryUsingFor pattern matches library_declaration bodies
// uniformly with contract_declaration / interface_declaration bodies
// (verified by reading queries.go). Pass 1.5 containerIDByFuncID maps
// every NodeFunction inside a library to that library's ID. bindings
// keyed by library ID get populated by V0 sweep. lookupReceiverType
// + resolveBindingLib pipeline therefore should work uniformly inside
// library bodies as well.
//
// V2.3 locks this expectation in with a fixture so future refactors
// don't accidentally exclude library-body using-for from the
// dispatch pipeline.
//
// V2.3 carry-over (V2.4+):
//   - Byte-range precision over V2.0's line-based fallback.
//   - Module/import additional patterns.
//   - Grammar-blocked items.

// TestUsingForV230_LibraryBodyUsing — library declares its own using-
// for binding and dispatches via that binding in its own function.
// WrapperLib.compute → InnerLib.add via `using InnerLib for uint256`
// inside WrapperLib.
func TestUsingForV230_LibraryBodyUsing(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v230", "library_body_using.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "WrapperLib.compute", target: "InnerLib.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V2.3 library body using-for) mismatch:\n got=%v\nwant=%v", got, want)
	}
}
