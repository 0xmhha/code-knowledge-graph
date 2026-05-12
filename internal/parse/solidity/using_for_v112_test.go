package solidity_test

import (
	"testing"
)

// W-C W6 V1.12 — generic member-chain walker tests.
//
// Shape: `<obj>.<f1>.<f2>...<fN>.<method>(...)` for N ≥ 3 (depth-3+
// pure member access chain). V1.10 catches N=1, V1.11 catches N=2;
// V1.12 fallback handles arbitrary depth via iterative walker through
// structFieldTypes.
//
// V1.12 carry-over (V1.13+): this-prefixed variants, multi-return
// tuple destructuring, cross-file struct validation.

// TestUsingForV112_Depth3MemberChain — canonical V1.12 case. depth-3
// chain `org.user.account.balance.add(1)`. V1.11 (depth-2) rejects;
// V1.12 fires.
func TestUsingForV112_Depth3MemberChain(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v112", "depth3_member_chain.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "Vault.run", target: "DeepLib.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (using-for V1.12 depth-3) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV112_Depth4MemberChain — arbitrary depth (N=4). Confirms
// V1.12 walker isn't capped at any specific depth.
func TestUsingForV112_Depth4MemberChain(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v112", "depth4_member_chain.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "Top.run", target: "MoreLib.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (using-for V1.12 depth-4) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV112_Depth3MidUnknown — depth-3 chain but middle hop's
// field doesn't exist on the parent struct. V1.12 walker drops.
func TestUsingForV112_Depth3MidUnknown(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v112", "depth3_mid_unknown.sol")
	got := collectUsingForCalls(nodes, edges)
	if len(got) != 0 {
		t.Errorf("unexpected V1.12 EdgeCalls when middle hop unknown: %v", got)
	}
}
