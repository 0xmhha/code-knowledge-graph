package solidity_test

import (
	"testing"
)

// W-C W6 V1.17 — Solidity shadowing precedence fix.
//
// Per Solidity scoping rules, function-scope (locals + parameters)
// shadows contract-scope (state variables). The inner scope wins on
// identifier resolution. Pre-V1.17 `lookupReceiverType` walked
// state-var → param → local-var, which is the REVERSE of Solidity
// semantics — when a local shadows a state-var with the same name,
// the pre-V1.17 chain would resolve to the state-var type and drop
// (or misroute) the dispatch.
//
// V1.17 corrects the precedence to local-var → param → state-var.
// V1.13 (`this.<state-var>.<method>`) is unaffected — `this.` is an
// explicit contract reference that bypasses the function scope.
//
// V1.17 carry-over (V1.18+):
//   - Pre-declared identifier-slot tuple (`(a, b) = pair()` with
//     `var` keyword) — practically irrelevant in modern Solidity
//     (`var` deprecated in 0.5.0+).
//   - Cross-file tuple validation (V1.14 idiom for V1.16).
//   - Block-scoped shadowing precision (V2+ refactor).
//   - Grammar-blocked items.

// TestUsingForV117_LocalShadowsState — canonical V1.17 case. State var
// `x` is a struct (no SafeMath binding); local var `x` in the function
// is `uint256` (bound to SafeMath). The receiver `x.add(1)` must
// resolve to the local (uint256), not the state (UserData). Pre-V1.17
// (state-var-first) produces a false negative; V1.17 fix produces the
// correct EdgeCalls.
func TestUsingForV117_LocalShadowsState(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v117", "local_shadows_state.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "C.f", target: "SafeMath.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V1.17 local shadows state) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV117_ParamShadowsState — function parameter shadows
// state-variable with the same name. param `uint256 x` must win over
// state `UserData x`. Locks the V1.1 path's precedence position.
func TestUsingForV117_ParamShadowsState(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v117", "param_shadows_state.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "C.f", target: "SafeMath.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V1.17 param shadows state) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV117_StateOnlyNoShadow — baseline regression guard: with
// no local/param of the same name, state-var resolution still works
// after the V1.17 precedence flip. Catches accidental over-reach of
// the fix.
func TestUsingForV117_StateOnlyNoShadow(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v117", "state_only_no_shadow.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "C.f", target: "SafeMath.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V1.17 no-shadow baseline) mismatch:\n got=%v\nwant=%v", got, want)
	}
}
