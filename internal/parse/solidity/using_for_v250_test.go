package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V2.5 — operator-form using directive grammar exploration.
//
// Solidity 0.8.13+ allows file-level `using {f1, f2} for T;` (function-
// alias form), and 0.8.19+ extends this with user-defined operator
// bindings: `using {add as +, sub as -} for uint256;`. Tree-sitter
// v1.2.13 has a `using_alias` node type that includes
// `user_definable_operator` children for the operator-form variant.
//
// V0 query (`(type_alias (identifier) @lib)`) matches only the
// type-alias variant (the legacy `using SafeMath for ...` form). The
// using_alias variant is ignored — V0 documented this as grammar /
// scope limitation. V2.5 explores empirically what happens when this
// grammar piece IS in source: does the file parse cleanly, are
// surrounding declarations still indexed, are there phantom edges?
//
// V2.5 locks the V0/V1/V2 behavior: operator-form directives produce
// 0 EdgeUsesFor (V0 query misses them) but the rest of the file
// (functions, contracts) still indexes normally. V3+ would need to
// either upgrade the grammar or extend queryUsingFor to match
// `using_alias` children.

// TestUsingForV250_OperatorFormLimitation — Operator-form `using
// {mul as *} for uint256 global;` doesn't emit EdgeUsesFor under
// V0 query. Caller `Calc.compute` uses operator `*` (which would
// dispatch to `mul` in Sol 0.8.19+ semantics) but our graph doesn't
// surface that linkage. Locks the known limitation.
func TestUsingForV250_OperatorFormLimitation(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v250", "operator_form.sol")

	// (a) No EdgeUsesFor from this file — operator-form directive
	// alone is the only `using` here, and V0 query misses it.
	for _, e := range edges {
		if e.Type == types.EdgeUsesFor {
			t.Errorf("unexpected EdgeUsesFor in operator-form fixture (V2.5 limitation lock): %+v", e)
		}
	}

	// (b) The function `mul` and contract `Calc` should still index
	// — the using directive's parsing state shouldn't cascade to
	// surrounding declarations.
	seenMul := false
	seenCalc := false
	seenCompute := false
	for _, n := range nodes {
		switch n.QualifiedName {
		case "mul":
			seenMul = true
		case "Calc":
			seenCalc = true
		case "Calc.compute":
			seenCompute = true
		}
	}
	if !seenMul {
		t.Errorf("free function `mul` not indexed (V2.5 surround-safety)")
	}
	if !seenCalc {
		t.Errorf("contract `Calc` not indexed (V2.5 surround-safety)")
	}
	if !seenCompute {
		t.Errorf("function `Calc.compute` not indexed (V2.5 surround-safety)")
	}
}
