package solidity_test

import (
	"testing"
)

// W-C W6 V1.15 — single-return local-var receiver tests.
//
// Shape: `Type x = expr; x.method(...)` where x is a function-local
// variable_declaration. New localVarTypes index (funcID, varName →
// typeName) plugs into the receiver-type lookup chain after
// stateVarTypes (V1.0) → paramTypes (V1.1). V1.10/V1.11/V1.12 struct-
// walker `obj` resolution shares the same three-tier fallback via the
// new `lookupReceiverType` helper.
//
// V1.13 (this-prefixed) intentionally bypasses the helper — `this`
// references the caller contract, so the named member must be a state
// variable, never a parameter or local.
//
// V1.15 carry-over (V1.16+):
//   - Multi-return tuple destructuring (variable_declaration_tuple).
//   - Local-var with RHS = function-call (use funcReturnTypes for
//     type inference instead of declared LHS — covered indirectly when
//     the LHS type is explicit).
//   - Block-scoped shadowing (V1.15 V0 treats locals as function-scoped).

// TestUsingForV115_LocalVarDirect — canonical V1.15 case. Local var
// `uint256 x` used as a V1.0-shape receiver (`x.add(1)`). Tests that
// localVarTypes plugs into resolveUsingForCallRef after stateVar/param
// miss.
func TestUsingForV115_LocalVarDirect(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v115", "local_var_direct.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "Calculator.compute", target: "SafeMath.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V1.15 direct local-var) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV115_LocalVarStruct — local-var receiver feeding the V1.10
// struct-field walker. `UserData memory u = ...; u.balance.add(1)`.
// Confirms localVarTypes fallback works for obj resolution in V1.10's
// chain too.
func TestUsingForV115_LocalVarStruct(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v115", "local_var_struct.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "Vault.handle", target: "SafeMath.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V1.15 + V1.10 struct walker) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV115_LocalVarUnknownType — local var whose declared type
// has no using-for binding. Resolver must drop — no false-positive
// EdgeCalls.
func TestUsingForV115_LocalVarUnknownType(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v115", "local_var_unknown_type.sol")
	got := collectUsingForCalls(nodes, edges)
	if len(got) != 0 {
		t.Errorf("unexpected V1.15 EdgeCalls when local-var type has no binding: %v", got)
	}
}
