package solidity_test

import (
	"testing"
)

// W-C W6 V1.19 — named return parameters → paramTypes.
//
// Solidity allows function return declarations to carry names:
// `function f() returns (uint256 result)`. `result` is then a function-
// scope variable initialised to zero, assignable and readable inside
// the function body. The pre-V1.19 parser indexed (paramName, type)
// only for direct parameter children of function_definition — return_
// type's parameter children were captured for their type by V1.3
// (funcReturnTypes) but their names were never paired into paramTypes.
//
// V1.19 closes the gap by extending the return_type walker to also
// emit dispatchKindUsingForParamType PendingRefs whenever a return
// parameter has a name field. Resolution path: lookupReceiverType
// (V1.17 ordered: local-var → param → state-var) picks up the named
// return param via paramTypes.
//
// V1.19 carry-over (V1.20+):
//   - Block-scoped shadowing precision (V2+ refactor).
//   - Grammar-blocked items (free-function form, file-level using).
//   - Module/import handling (V2 territory).

// TestUsingForV119_NamedReturnParam — canonical V1.19 case. Function
// `f() returns (uint256 result)`. Inside f(), `result.add(1)` must
// resolve via paramTypes (V1.19 fix) → uint256 → SafeMath.add.
// Pre-V1.19 produces a false negative.
func TestUsingForV119_NamedReturnParam(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v119", "named_return.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "C.f", target: "SafeMath.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V1.19 named return param) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV119_NamedReturnMulti — multiple named return parameters.
// Function `f() returns (uint256 a, uint256 b)`. Inside f(), `a.add(b)`
// uses both — V1.19 must emit a paramType PendingRef for each named
// slot, not just the first.
func TestUsingForV119_NamedReturnMulti(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v119", "named_return_multi.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "C.f", target: "SafeMath.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V1.19 multi named return param) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV119_AnonymousReturn — baseline guard: a return slot
// without a name field emits nothing through V1.19 (anonymous slot
// has no addressable identifier). V1.3 funcReturnTypes still fires
// for chain-call dispatch separately. Confirms over-reach 0.
func TestUsingForV119_AnonymousReturn(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v119", "anonymous_return.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "C.f", target: "SafeMath.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V1.19 anonymous return baseline) mismatch:\n got=%v\nwant=%v", got, want)
	}
}
