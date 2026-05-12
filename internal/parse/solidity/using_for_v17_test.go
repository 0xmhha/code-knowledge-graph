package solidity_test

import (
	"testing"
)

// W-C W6 V1.7 — depth-3 same-contract chained dispatch tests.
//
// Shape: `<fn1>().<fn2>().<fn3>().<method>(...)`. Three levels of
// funcReturnTypes walks. V1.8+ promotes to generic depth-N walker
// (and cross-contract variant of the same depth).
//
// V1.7 carry-over (V1.8+): depth ≥ 4 generic chains, generic walker
// refactor (subsume V1.3/V1.5/V1.7), multi-return tuple slot.

// TestUsingForV17_TripleChainBasic — canonical V1.7 case.
// `mkA().mkB().mkC().add(1)` — three local helper chains through
// Stage1 → Stage2 → uint256, then .add(1) resolves via uint256 binding.
func TestUsingForV17_TripleChainBasic(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v17", "triple_chain_basic.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "Vault.run", target: "ChainLib.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (using-for triple-chain) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV17_TripleChainMiddleUnknown — middle step (link 2) not
// found on inner function's return type. V1.7 must drop.
func TestUsingForV17_TripleChainMiddleUnknown(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v17", "triple_chain_middle_unknown.sol")
	got := collectUsingForCalls(nodes, edges)
	if len(got) != 0 {
		t.Errorf("unexpected V1.7 EdgeCalls when middle step unknown: %v", got)
	}
}

// TestUsingForV17_TripleChainNoBinding — chain links resolve all the
// way through but the final type has no binding. Resolver must drop.
func TestUsingForV17_TripleChainNoBinding(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v17", "triple_chain_no_binding.sol")
	got := collectUsingForCalls(nodes, edges)
	if len(got) != 0 {
		t.Errorf("unexpected V1.7 EdgeCalls when binding misses: %v", got)
	}
}
