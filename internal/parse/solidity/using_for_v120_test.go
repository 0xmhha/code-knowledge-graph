package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V1.20 — additional name-scope captures.
//
// Two gaps the V1.15-V1.19 family didn't cover:
//
//  1. for-loop init variable: `for (uint256 i = 0; ...)` declares `i`
//     inside the for_statement's init slot. V1.15's recursive descent
//     through function body should reach this variable_declaration_
//     statement.
//
//  2. try/catch returns clause: `try foo() returns (uint256 r) { ... }`
//     binds `r` to the success block. Tree-sitter exposes the returns
//     clause as `parameter` children of try_statement — distinct from
//     function_definition's `return_type` field, which V1.19 walks.
//
// V1.20 carry-over (V1.21+):
//   - catch_clause's named parameter (`catch Error(string memory s)`).
//   - Block-scoped shadowing precision (V2+ refactor).
//   - Module/import handling (V2 territory).
//   - Grammar-blocked items.

// TestUsingForV120_ForLoopInit — for-loop init variable as using-for
// receiver. V1.15 recursive descent already reaches variable_declaration
// _statement inside for_statement init slot — this test is a regression
// guard that the descent stays in place after future refactors.
func TestUsingForV120_ForLoopInit(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v120", "for_loop_init.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "C.f", target: "SafeMath.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V1.20 for-loop init) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV120_TryCatchReturns — try/catch returns clause named
// parameter. Pre-V1.20 false-negative: V1.19 only walked function_
// definition.return_type. V1.20 fix walks try_statement's direct
// parameter children too.
func TestUsingForV120_TryCatchReturns(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v120", "try_catch_returns.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "C.f", target: "SafeMath.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V1.20 try-catch returns) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV120_TryCatchAnonymousReturns — try/catch with anonymous
// returns slot. V1.20 emit must skip (no name field → no addressable
// receiver). Over-reach guard.
func TestUsingForV120_TryCatchAnonymousReturns(t *testing.T) {
	_, edges := parseResolveOneSol(t, "testdata/using_for_v120", "try_catch_anonymous_returns.sol")
	for _, e := range edges {
		if e.Type == types.EdgeCalls {
			// V1.20 shouldn't surface an EdgeCalls for try-returns
			// here — there's no `<receiver>.method()` for V1.x to
			// dispatch on (the success block returns a literal).
			t.Errorf("unexpected EdgeCalls in anonymous-returns body: %+v", e)
		}
	}
}
