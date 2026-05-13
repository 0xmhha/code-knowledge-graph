package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V2.9 — contract-scope bare free-function alias probe.
//
// Completes the alias-shape axis at contract scope. V2.6 / V2.7 /
// V2.9 form a triplet:
//
//   alias-entry shape              | example                | result
//   -------------------------------+------------------------+--------
//   library-qualified (Lib.member) | {Math.add, Math.sub}   | V2.6: 1
//   operator-form (Lib.m as +)     | {Math.add as +}        | V2.7: 0
//   bare (free-fn name only)       | {addPlusOne}           | V2.9: ?
//
// V0 query `(using_directive (type_alias (identifier) @lib) ...)` was
// shown by V2.6 to incidentally match library-qualified entries —
// tree-sitter v1.2.13 wraps `Lib.member` such that `Lib` is captured
// as the `@lib` identifier. V2.7 showed the operator suffix breaks
// that match by changing the alias-entry node shape.
//
// V2.9 asks whether the bare form (no qualifier, no operator)
// matches V0. The fixture predicts 0 EdgeUsesFor under both
// hypotheses:
//   - If V0 captures `addPlusOne` as @lib, the byName lookup
//     (expects a Contract / library) fails → PendingRef stays
//     unresolved.
//   - If V0 doesn't capture at all, no PendingRef is emitted in
//     the first place.
//
// Either way, the bare alias form has no library to bind to, so
// the using-for binding can't surface as an EdgeUsesFor in the
// current schema (EdgeUsesFor is Contract → Contract/library).

// TestUsingForV290_ContractScopeBareFunctionAlias — `contract Calc
// { using {addPlusOne} for uint256; }`. Locks empirical V0/V1/V2
// behavior for the bare free-function alias form.
//
// First run on 2026-05-13 (tree-sitter-solidity v1.2.13):
//   - 0 EdgeUsesFor — bare alias entry can't produce a valid
//     EdgeUsesFor (no library to point to).
//   - 0 EdgeCalls via using-for path — V1.0+ dispatch chain has
//     no `lib.method` resolution candidate for a bare alias.
//
// Surround-safety: free function `addPlusOne` (qname identical to
// its name since it lives at file scope), contract `Calc`, and
// function `Calc.compute` should all still index.
//
// Triplet result at contract scope (empirically locked):
//   V2.6 library-qualified  → 1 EdgeUsesFor (V0 incidental)
//   V2.7 operator-form      → 0 EdgeUsesFor (AST shape)
//   V2.9 bare free-function → 0 EdgeUsesFor (no library target)
func TestUsingForV290_ContractScopeBareFunctionAlias(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v290", "probe_bare_function_alias.sol")

	// (a) Lock: 0 EdgeUsesFor for bare free-function alias.
	edgeCount := 0
	for _, e := range edges {
		if e.Type == types.EdgeUsesFor {
			edgeCount++
		}
	}
	if edgeCount != 0 {
		t.Errorf("expected 0 EdgeUsesFor for V2.9 bare alias, got %d", edgeCount)
		for _, e := range edges {
			if e.Type == types.EdgeUsesFor {
				t.Logf("  unexpected edge: %+v", e)
			}
		}
	}

	// (b) Lock: 0 EdgeCalls via using-for path. The bare alias
	// can't drive V1.0 dispatch since there's no library to look
	// up `addPlusOne.addPlusOne` against. The naked receiver call
	// `x.addPlusOne()` may surface as something else (e.g. an
	// AMBIGUOUS unresolved call), but it must not produce a using-
	// for-mediated EdgeCalls from `Calc.compute` to `addPlusOne`.
	qnameByID := map[string]string{}
	for _, n := range nodes {
		qnameByID[n.ID] = n.QualifiedName
	}
	for _, e := range edges {
		if e.Type != types.EdgeCalls {
			continue
		}
		srcQ := qnameByID[e.Src]
		dstQ := qnameByID[e.Dst]
		if srcQ == "Calc.compute" && dstQ == "addPlusOne" {
			t.Errorf("unexpected EdgeCalls Calc.compute → addPlusOne (V2.9 bare alias should not drive dispatch): %+v", e)
		}
	}

	// (c) Surround-safety: all declarations index.
	seenFreeFn := false
	seenCalc := false
	seenCompute := false
	for _, n := range nodes {
		switch n.QualifiedName {
		case "addPlusOne":
			seenFreeFn = true
		case "Calc":
			seenCalc = true
		case "Calc.compute":
			seenCompute = true
		}
	}
	if !seenFreeFn {
		t.Errorf("free function `addPlusOne` not indexed (V2.9 surround-safety)")
	}
	if !seenCalc {
		t.Errorf("contract `Calc` not indexed (V2.9 surround-safety)")
	}
	if !seenCompute {
		t.Errorf("function `Calc.compute` not indexed (V2.9 surround-safety)")
	}
}
