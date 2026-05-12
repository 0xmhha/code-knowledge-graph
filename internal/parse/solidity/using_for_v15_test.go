package solidity_test

import (
	"testing"
)

// W-C W6 V1.5 — depth-2 chained dispatch tests.
//
// Shape: `<innerFn1>().<innerFn2>().<method>(...)`. Resolver walks two
// levels of funcReturnTypes (innerFn1 → returnType1 → innerFn2 in
// returnType1's namespace → returnType2 → binding lookup).
//
// V1.5 carry-over (V1.6+): cross-contract deep chains
// (`obj.foo().bar().baz()`), depth >= 3 chains, multi-return tuple
// slot selection.

// TestUsingForV15_DeepChainBasic — canonical V1.5 case.
// `mkStep1().step2().add(1)` — chain resolves through
// mkStep1's Stage return → Stage.step2's uint256 return →
// using ChainLib for uint256 → ChainLib.add.
func TestUsingForV15_DeepChainBasic(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v15", "deep_chain_basic.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "Vault.run", target: "ChainLib.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (using-for deep-chain) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV15_DeepChainMiddleUnknown — middle step's method doesn't
// exist on the inner function's return type. V1.5 must drop the chain
// without false-positive emission.
func TestUsingForV15_DeepChainMiddleUnknown(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v15", "deep_chain_middle_unknown.sol")
	got := collectUsingForCalls(nodes, edges)
	if len(got) != 0 {
		t.Errorf("unexpected V1.5 EdgeCalls when middle step unknown: %v", got)
	}
}

// TestUsingForV15_DeepChainPrimitiveMiddle — innermost function returns
// a primitive type (uint256). Middle step's namespace lookup
// (`uint256.foo`) misses — V1.5 drops cleanly even though the predicate
// matches syntactically.
func TestUsingForV15_DeepChainPrimitiveMiddle(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v15", "deep_chain_primitive_middle.sol")
	got := collectUsingForCalls(nodes, edges)
	if len(got) != 0 {
		t.Errorf("unexpected V1.5 EdgeCalls when innermost return is primitive: %v", got)
	}
}
