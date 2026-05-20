package solidity_test

import (
	"testing"
)

// W-C W6 V1.22 — modifier body scope captures.
//
// Pre-V1.22, parameter and local-var meta emission ran only for
// function_definition nodes (via runFunctionDecl). modifier_definition
// shares the same AST shape (body field + direct `parameter` children)
// but was emitted as a generic NodeModifier (via runDecl) — no
// PendingRefs flowed into paramTypes / localVarTypes for its body.
// Result: when a modifier body used a parameter or local as a using-
// for receiver, the dispatch dropped (false-negative).
//
// V1.22 closes this for modifier_definition by:
//   1. Promoting modifier qname to "Contract.modifier" so the
//      Pass 1.5 containerIDByFuncID loop catches it (mirroring
//      function_definition's qname prefix).
//   2. Adding a dedicated walker (runModifierMeta) that runs the
//      V1.1 / V1.15 / V1.19 meta-emit pipeline against each
//      modifier_definition node.
//
// V1.22 carry-over (V1.23+):
//   - constructor_definition (not yet a graph node — needs query +
//     node emission first, then meta walker).
//   - fallback_receive_definition (same AST family).
//   - Free function parameters (file-level function_definition in
//     0.7.4+; runFunctionDecl may already cover it — to verify).
//   - Block-scoped shadowing precision (V2+).
//   - Module/import handling (V2 territory).

// TestUsingForV122_ModifierParam — modifier parameter as receiver.
// Pre-V1.22 false-negative: emitParameterMetaPending only ran for
// function_definition. V1.22 fix routes through a generalized walker
// to cover modifier_definition's `parameter` children too.
func TestUsingForV122_ModifierParam(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v122", "modifier_param.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "C.hasBalance", target: "SafeMath.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V1.22 modifier param) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV122_ModifierLocal — local var declared inside a modifier
// body as receiver. Pre-V1.22 false-negative: emitLocalVarMetaPending
// only ran for function_definition.
func TestUsingForV122_ModifierLocal(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v122", "modifier_local.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "C.checkAmount", target: "SafeMath.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V1.22 modifier local) mismatch:\n got=%v\nwant=%v", got, want)
	}
}
