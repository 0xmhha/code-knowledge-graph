package solidity_test

import (
	"testing"
)

// W-C W6 V1.1 — function-parameter receiver dispatch tests.
//
// Builds on V1.0 (state-variable receiver). The resolver tries state-var
// first, then falls back to parameter — Solidity scoping forbids a
// parameter name from shadowing a state variable in receiver position,
// so the lookup order is purely a hot-path optimisation, not a
// correctness lever.

// TestUsingForV11_ParamReceiverDispatch — canonical V1.1 case. A pure
// function whose receiver is a parameter (`x.times(2)`) resolves through
// the parameter-type index instead of stateVarTypes.
func TestUsingForV11_ParamReceiverDispatch(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v11", "param_receiver.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "Calc.double", target: "Math.times"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (using-for) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV11_StateAndParamMixed — both receivers in the same
// contract emit independent EdgeCalls. The state-var path and the
// parameter path must coexist without one masking the other.
func TestUsingForV11_StateAndParamMixed(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v11", "state_and_param.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "Mixed.bumpParam", target: "Helper.bump"},
		{caller: "Mixed.bumpState", target: "Helper.bump"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (using-for) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV11_AnonymousParamSkipped — parameters without a name
// field generate no paramTypes entry, so anonymous receivers can never
// trigger using-for dispatch. The fixture has a `uint256` parameter
// with no identifier; the resolver must produce zero using_for_call
// EdgeCalls.
func TestUsingForV11_AnonymousParamSkipped(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v11", "anonymous_param.sol")
	got := collectUsingForCalls(nodes, edges)
	if len(got) != 0 {
		t.Errorf("unexpected using-for EdgeCalls for anonymous-param fixture: %v", got)
	}
}
