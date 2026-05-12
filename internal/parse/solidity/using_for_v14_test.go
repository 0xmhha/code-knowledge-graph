package solidity_test

import (
	"testing"
)

// W-C W6 V1.4 — cross-contract chained dispatch tests.
//
// Shape: `<obj>.<innerFn>().<method>(...)`. Resolver walks the
// receiver's typeName (state-var or parameter) to find the inner
// function in the receiver's contract / interface, then chains the
// inner function's return type through the using-for binding map.
//
// V1.4 carry-over (V1.5+): deeper chains (`obj.foo().bar().baz()`),
// multi-return tuple slot selection.

// TestUsingForV14_CrossChainBasic — canonical V1.4 case.
// `factory.create().bump()` where factory is a state var of contract
// type Factory, create() returns uint256, and using ChainLib for uint256
// binds the chain to ChainLib.bump.
func TestUsingForV14_CrossChainBasic(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v14", "cross_chain_basic.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "Vault.run", target: "ChainLib.bump"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (using-for cross-chain) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV14_CrossChainUnknownMethod — receiver contract has no
// matching inner method (bogus()). Inner-step `funcByQName` lookup
// misses → drop, no false-positive emission.
func TestUsingForV14_CrossChainUnknownMethod(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v14", "cross_chain_unknown_method.sol")
	got := collectUsingForCalls(nodes, edges)
	if len(got) != 0 {
		t.Errorf("unexpected V1.4 EdgeCalls when inner method unknown: %v", got)
	}
}

// TestUsingForV14_CrossChainPrimitiveDrop — receiver type is uint256
// (primitive). Cross-chain predicate matches structurally but the
// inner-step lookup `funcByQName["uint256.unknownFn"]` misses (no
// container named uint256), so the chain drops cleanly.
func TestUsingForV14_CrossChainPrimitiveDrop(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v14", "cross_chain_primitive_drop.sol")
	got := collectUsingForCalls(nodes, edges)
	if len(got) != 0 {
		t.Errorf("unexpected V1.4 EdgeCalls when receiver is primitive: %v", got)
	}
}
