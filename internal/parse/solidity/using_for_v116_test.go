package solidity_test

import (
	"testing"
)

// W-C W6 V1.16 — multi-return tuple destructuring tests.
//
// Shape: `(Ta a, Tb b) = foo(); a.method(...)` — variable_declaration_
// statement wraps a variable_declaration_tuple whose children are mixed
// `variable_declaration` (typed slot — V1.16 scope) and `identifier`
// (pre-declared slot — V1.17+ scope, dropped here without RHS multi-
// slot fnReturnTypes inference).
//
// V1.16 reuses V1.15's localVarTypes infrastructure: each typed tuple
// slot emits the same (varName, typeName) PendingRef as a single-var
// declaration, so V1.0/V1.10/V1.11/V1.12 resolvers automatically pick
// it up via the existing lookupReceiverType chain.
//
// V1.16 carry-over (V1.17+):
//   - Pre-declared identifier slots: need multi-slot funcReturnTypes
//     (extend V1.3's funcReturnTypes to (funcID, slotIndex) → typeName).
//   - Wildcard slots (`(uint256 a, ) = pair()` — the empty slot is
//     skipped here without ceremony; no PendingRef emitted, no impact).

// TestUsingForV116_TupleBasic — canonical V1.16 case. Tuple LHS with
// two typed slots; first slot used as V1.0-shape receiver
// (`a.add(1)`). Confirms V1.16 fans out PendingRefs through
// variable_declaration_tuple's variable_declaration children.
func TestUsingForV116_TupleBasic(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v116", "tuple_basic.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "Vault.run", target: "SafeMath.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V1.16 tuple basic) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV116_TupleStructSlot — V1.16 tuple slot is a struct, feeds
// into V1.10 struct-field walker. Confirms that the localVarTypes
// emitted from tuple slots interoperate with the V1.10 obj resolution
// chain via lookupReceiverType (introduced in V1.15).
func TestUsingForV116_TupleStructSlot(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v116", "tuple_struct_slot.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "Handler.process", target: "SafeMath.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V1.16 + V1.10 struct slot) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV116_TupleUnboundSlot — typed tuple slot whose type has
// no using-for binding (bytes32 here, with only `using SafeMath for
// uint256`). Negative guard: only the bound slot's receiver emits an
// EdgeCalls; the unbound slot's receiver would drop in resolution.
func TestUsingForV116_TupleUnboundSlot(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v116", "tuple_unbound_slot.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "Caller.run", target: "SafeMath.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V1.16 unbound-slot guard) mismatch:\n got=%v\nwant=%v", got, want)
	}
}
