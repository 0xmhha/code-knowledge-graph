package solidity_test

import (
	"testing"
)

// W-C W6 V1.11 — depth-2 nested struct field receiver dispatch tests.
//
// Shape: `<obj>.<field1>.<field2>.<method>(...)`. Resolver walks
// structFieldTypes twice — obj's struct field1 → field1Type's struct
// field2 → binding lookup on field2's type.
//
// V1.11 carry-over (V1.12+): depth ≥ 3 nested struct fields, multi-
// return tuple destructuring, mixed receiver chains (struct field
// followed by method call).

// TestUsingForV111_NestedStructFieldBasic — canonical V1.11 case.
// `user.account.balance.add(1)` resolves through user's UserData type
// to account's Account type to balance's uint256 type to NestedLib.add.
func TestUsingForV111_NestedStructFieldBasic(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v111", "nested_struct_field_basic.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "Vault.run", target: "NestedLib.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (using-for nested struct field) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV111_NestedStructFieldInnerUnknown — outer field is a
// struct but inner field doesn't exist on it. V1.11 step 4 drops.
func TestUsingForV111_NestedStructFieldInnerUnknown(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v111", "nested_struct_field_inner_unknown.sol")
	got := collectUsingForCalls(nodes, edges)
	if len(got) != 0 {
		t.Errorf("unexpected V1.11 EdgeCalls when inner field unknown: %v", got)
	}
}

// TestUsingForV111_NestedStructFieldOuterNotStruct — middle field is a
// primitive (uint256). V1.11 expects field1Type to be a known struct;
// primitive miss → drop.
func TestUsingForV111_NestedStructFieldOuterNotStruct(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v111", "nested_struct_field_outer_not_struct.sol")
	got := collectUsingForCalls(nodes, edges)
	if len(got) != 0 {
		t.Errorf("unexpected V1.11 EdgeCalls when middle field is primitive: %v", got)
	}
}
