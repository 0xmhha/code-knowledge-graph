package solidity_test

import (
	"testing"
)

// W-C W6 V1.24 — fallback_receive_definition graph node + meta walker.
//
// Solidity's `fallback() { ... }` and `receive() external payable { ... }`
// are both parsed by tree-sitter-solidity v1.2.13 as the same node kind
// `fallback_receive_definition`. The grammar has no field that
// disambiguates them — V1.24 reads the source-text leading token to
// pick the synthetic name ("fallback" or "receive").
//
// Pre-V1.24 there was no graph node for either, so any using-for
// receiver in their body dropped. V1.24 adds:
//   1. queryFallbackReceive — matches both forms.
//   2. runFallbackReceiveDecl — synthesises name from source text,
//      emits NodeFunction with SubKind="fallback" or "receive", qname
//      "<Container>.fallback" / "<Container>.receive".
//   3. emitParameterMetaPending + emitLocalVarMetaPending called per
//      definition (fallback can carry params in 0.6+; receive cannot
//      by language rule, so its parameter walk is naturally empty).
//   4. nearestFunctionQnameAndStart recognises fallback_receive_definition.
//
// V1.24 carry-over (V1.25+):
//   - Free function parameters (file-level function_definition in
//     0.7.4+; runFunctionDecl may already cover — to verify).
//   - Block-scoped shadowing precision (V2+ refactor).
//   - Module/import handling (V2 territory).

// TestUsingForV124_ReceiveLocal — receive() body local var as receiver.
// receive() takes no parameters by language rule, so the test focuses
// on local-var capture.
func TestUsingForV124_ReceiveLocal(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v124", "receive_local.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "C.receive", target: "SafeMath.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V1.24 receive local) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV124_FallbackLocal — fallback() body local var as
// receiver. Confirms V1.24 synthesises the right name for the fallback
// half of the union.
func TestUsingForV124_FallbackLocal(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v124", "fallback_param.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "C.fallback", target: "SafeMath.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V1.24 fallback local) mismatch:\n got=%v\nwant=%v", got, want)
	}
}
