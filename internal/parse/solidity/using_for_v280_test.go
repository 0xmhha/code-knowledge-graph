package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V2.8 — file-level free-function form using directive probe.
//
// Completes the 2x2 (scope × alias-shape) matrix that V2.5 / V2.6 /
// V2.7 incrementally locked:
//
//                  | free-function          | operator-form
//   ---------------+------------------------+----------------------
//   file-level     | V2.8 (this)            | V2.5: 0 edges (scope)
//   contract-scope | V2.6: 1 edge (V0 inc.) | V2.7: 0 edges (shape)
//
// V0 queryUsingFor matches inside `(contract_body ...)` only —
// `contract_declaration` / `library_declaration` /
// `interface_declaration`. A `using_directive` that's a direct child
// of `source_file` (file-level scope) cannot satisfy any of the
// three top-level alternatives, regardless of the alias-entry shape
// (`type_alias` vs `using_alias`). Therefore V2.8 should match
// V2.5's outcome — 0 EdgeUsesFor from scope exclusion — and the
// alias-shape axis becomes irrelevant once scope already excludes.
//
// Additionally, the V0 + V1.2 grammar limitation note (queries.go
// preamble, 2026-05-12) recorded that tree-sitter-solidity v1.2.13
// "wraps such [file-level using] directives in ERROR nodes." V2.5
// already showed (operator-form variant) that the ERROR cascade
// does not contaminate surrounding declarations. V2.8 confirms the
// same holds for the free-function variant: library, library's
// function, contract, and contract's function all still index.

// TestUsingForV280_FileLevelFreeFunctionForm — `using {Math.add} for
// uint256 global;` at file scope. Locks 0-edges + surround-safety.
//
// First run on 2026-05-13 (tree-sitter-solidity v1.2.13):
//   - 0 EdgeUsesFor — V0 query's three contract-body alternatives
//     don't match a file-level `using_directive`. Result is identical
//     to V2.5's file-level operator-form, confirming that scope is
//     the dominant axis: once file-level scope is excluded, the
//     alias-entry shape (free-function vs operator) is irrelevant.
//
// Matrix completion table (all four quadrants empirically locked):
//   V2.5 file-level   + operator-form     → 0 edges (scope)
//   V2.6 contract-sc. + free-function     → 1 edge  (V0 incidental)
//   V2.7 contract-sc. + operator-form     → 0 edges (AST shape)
//   V2.8 file-level   + free-function     → 0 edges (scope)
//
// Surround-safety: library `Math`, function `Math.add`, contract
// `Calc`, and function `Calc.compute` must all still index. If any
// fails, that means the file-level using_directive's ERROR-wrapped
// AST cascades to siblings — and we'd need to investigate.
func TestUsingForV280_FileLevelFreeFunctionForm(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v280", "probe_file_level_free_function.sol")

	// (a) Lock: 0 EdgeUsesFor for file-level free-function form.
	edgeCount := 0
	for _, e := range edges {
		if e.Type == types.EdgeUsesFor {
			edgeCount++
		}
	}
	if edgeCount != 0 {
		t.Errorf("expected 0 EdgeUsesFor for V2.8 file-level free-function form, got %d", edgeCount)
		for _, e := range edges {
			if e.Type == types.EdgeUsesFor {
				t.Logf("  unexpected edge: %+v", e)
			}
		}
	}

	// (b) Surround-safety: all surrounding declarations still indexed.
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
		t.Errorf("library `Math` not indexed (V2.8 surround-safety)")
	}
	if !seenAdd {
		t.Errorf("function `Math.add` not indexed (V2.8 surround-safety)")
	}
	if !seenCalc {
		t.Errorf("contract `Calc` not indexed (V2.8 surround-safety)")
	}
	if !seenCompute {
		t.Errorf("function `Calc.compute` not indexed (V2.8 surround-safety)")
	}
}
