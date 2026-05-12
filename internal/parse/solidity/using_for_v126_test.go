package solidity_test

import (
	"testing"
)

// W-C W6 V1.26 — abstract contract body scope regression guard.
//
// Sol's `abstract contract Base { ... }` shares the
// contract_declaration AST kind with regular contracts; the `abstract`
// keyword is captured as a SubKind on NodeContract by W4
// (abstract_library.go, 2026-05-11). All V1.x using-for indexing
// (binding map, paramTypes, localVarTypes, containerIDByFuncID) keys
// off contract_declaration name, so abstract contracts should be
// indistinguishable from regular contracts for dispatch purposes.
//
// V1.26 locks this in with a minimal fixture so future refactors
// don't accidentally exclude abstract contracts.
//
// V1.26 carry-over (V1.27+):
//   - Inherited modifier `using` baseline regression guard.
//   - Block-scoped shadowing precision (V2+ refactor).
//   - Module/import handling (V2 territory).

// TestUsingForV126_AbstractContract — abstract contract with using-for
// binding and method-call dispatch in body. Asserts the abstract
// keyword doesn't disrupt V1.0 / V1.15 / V1.17 paths.
func TestUsingForV126_AbstractContract(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v126", "abstract_using.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "Base.computeBase", target: "SafeMath.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V1.26 abstract contract) mismatch:\n got=%v\nwant=%v", got, want)
	}
}
