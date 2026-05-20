package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V2.7 — contract-scope operator-form using directive probe.
//
// V2.5 locked file-level operator-form behavior:
//   `using {mul as *} for uint256 global;`  → 0 EdgeUsesFor
//   Reason: queryUsingFor only matches inside contract/library/
//   interface bodies; file-level scope is intentionally excluded.
//
// V2.6 rediscovered that contract-scope free-function form is
// incidentally captured:
//   `contract C { using {Math.add, Math.sub} for uint256; }`
//                                                → 1 EdgeUsesFor
//   Reason: tree-sitter v1.2.13 wraps the alias-list entry's
//   qualifier identifier inside a `type_alias` node, and V0
//   query `(type_alias (identifier) @lib)` matches it as @lib.
//
// V2.7 mirrors V2.6 with the operator-form variant — same scope
// (contract body), different alias structure (`as +` adds a
// `user_definable_operator` child). The empirical question: does
// the operator suffix break the type_alias wrapping that V2.6
// rediscovered, or does V0 still incidentally match?
//
// The test below executes the parser+resolver pipeline and locks
// whichever behavior emerges so future grammar bumps / query
// edits don't silently flip the result.

// TestUsingForV270_ContractScopeOperatorForm — `contract Calc {
// using {Math.add as +} for uint256; }`. Locks empirical V0
// behavior. Whether 0 or 1 edges, the test pins the count and
// (if any) the contract → library pair.
//
// First run on 2026-05-13 (tree-sitter-solidity v1.2.13):
//   - 0 EdgeUsesFor — operator-form's `user_definable_operator`
//     child changes the alias-entry AST shape enough that V0's
//     `(type_alias (identifier) @lib)` no longer matches. The
//     operator suffix is the discriminator vs. V2.6.
//
// Contrast table:
//
//	V2.5 file-level   + operator-form     → 0 edges (scope)
//	V2.6 contract-sc. + free-function     → 1 edge  (incidental)
//	V2.7 contract-sc. + operator-form     → 0 edges (AST shape)
//
// Surround-safety: the function `Math.add`, library `Math`, and
// contract `Calc` (with `Calc.compute`) must all still index;
// the using directive's parse shape shouldn't cascade.
func TestUsingForV270_ContractScopeOperatorForm(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v270", "probe_operator_form.sol")

	// (a) Lock (W6 V2.20 flip 2026-05-18): 1 EdgeUsesFor for
	// contract-scope operator-form. V2.20 added a recovery walker
	// that pattern-matches the misparsed `state_variable_declaration`
	// shape and emits the binding pair runUsingFor produces.
	edgeCount := 0
	for _, e := range edges {
		if e.Type == types.EdgeUsesFor {
			edgeCount++
		}
	}
	if edgeCount != 1 {
		t.Errorf("expected 1 EdgeUsesFor for V2.7 operator-form (contract scope, post-V2.20 recovery), got %d", edgeCount)
		for _, e := range edges {
			if e.Type == types.EdgeUsesFor {
				t.Logf("  edge: %+v", e)
			}
		}
	}

	// (b) Surround-safety: declarations still indexed.
	seenLib := false
	seenAdd := false
	seenCalc := false
	seenCompute := false
	for _, n := range nodes {
		switch n.QualifiedName {
		case "Math":
			seenLib = true
		case "Math.add":
			seenAdd = true
		case "Calc":
			seenCalc = true
		case "Calc.compute":
			seenCompute = true
		}
	}
	if !seenLib {
		t.Errorf("library `Math` not indexed (V2.7 surround-safety)")
	}
	if !seenAdd {
		t.Errorf("function `Math.add` not indexed (V2.7 surround-safety)")
	}
	if !seenCalc {
		t.Errorf("contract `Calc` not indexed (V2.7 surround-safety)")
	}
	if !seenCompute {
		t.Errorf("function `Calc.compute` not indexed (V2.7 surround-safety)")
	}
}
