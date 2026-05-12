package solidity_test

import (
	"testing"
)

// W-C W6 V1.10 — struct-field receiver dispatch tests.
//
// Shape: `<obj>.<field>.<method>(...)`. obj is a state-var or
// parameter whose type is a struct; field is a member of that struct.
// Resolver chains obj → objType → struct field → fieldType → binding
// lookup.
//
// V1.9 catches `this.<field>` first (inner.object = "this"); V1.10
// catches all other identifier-receiver shapes. V1.10 only fires when
// objType is a known struct in the structFieldTypes index.
//
// V1.10 carry-over (V1.11+): cross-file struct definitions, nested
// struct field receivers (`obj.outer.inner.method()`), multi-return
// tuple destructuring.

// TestUsingForV110_StructFieldBasic — canonical V1.10 case.
// `info.amount.add(1)` resolves through info's UserInfo struct type
// to amount's uint256 field type to StructLib.add.
func TestUsingForV110_StructFieldBasic(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v110", "struct_field_basic.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "Vault.run", target: "StructLib.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (using-for struct-field) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV110_StructFieldUnknown — `data.missingField.tag()`. obj
// is a known struct state-var, but the field doesn't exist on the
// struct. V1.10 must drop without false-positive emission.
func TestUsingForV110_StructFieldUnknown(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v110", "struct_field_unknown.sol")
	got := collectUsingForCalls(nodes, edges)
	if len(got) != 0 {
		t.Errorf("unexpected V1.10 EdgeCalls when struct field missing: %v", got)
	}
}

// TestUsingForV110_StructFieldParam — struct receiver is a function
// parameter (not state-var). Resolver falls back to paramTypes via the
// V1.0/V1.1 idiom shared by other V1.x branches.
func TestUsingForV110_StructFieldParam(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v110", "struct_field_param.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "ParamUser.process", target: "ParamLib.bump"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (using-for struct-field param) mismatch:\n got=%v\nwant=%v", got, want)
	}
}
