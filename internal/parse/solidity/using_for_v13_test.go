package solidity_test

import (
	"testing"
)

// W-C W6 V1.3 — return-value chaining tests.
//
// Chained call dispatch: `<innerFn>().<method>(...)`. The resolver
// recovers the inner function's return type via funcReturnTypes and
// treats it as the receiver type for binding lookup. V1.3 V0 supports
// only same-contract inner functions (cross-contract chaining
// `obj.foo().bar()` is V1.4+).
//
// File-level using directive and free-function form using_alias are
// blocked by tree-sitter-solidity v1.2.13 grammar limitations
// (ERROR-node parse on 0.8.13+ syntax). Both remain V1.x carry-over.

// TestUsingForV13_ReturnChainBasic — canonical V1.3 case. `factory()`
// returns uint256; `.add(1)` resolves through the using-for binding for
// uint256 to ChainLib.add.
func TestUsingForV13_ReturnChainBasic(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v13", "return_chain_basic.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "Vault.run", target: "ChainLib.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (using-for chained) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV13_ReturnChainNoBinding — chained call whose inner
// function's return type has no binding declared. Resolver must drop
// (the `using AddrLib for uint256;` declaration does NOT cover
// `address`, which is what `factory()` actually returns).
func TestUsingForV13_ReturnChainNoBinding(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v13", "return_chain_no_binding.sol")
	got := collectUsingForCalls(nodes, edges)
	if len(got) != 0 {
		t.Errorf("unexpected V1.3 EdgeCalls when no binding for return type: %v", got)
	}
}

// TestUsingForV13_ReturnChainUnknownFn — chained call where the inner
// identifier doesn't resolve to a known function in the build. V1.3
// must drop (innerFnName → funcByQName lookup miss).
func TestUsingForV13_ReturnChainUnknownFn(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v13", "return_chain_unknown_fn.sol")
	got := collectUsingForCalls(nodes, edges)
	if len(got) != 0 {
		t.Errorf("unexpected V1.3 EdgeCalls when inner function is unknown: %v", got)
	}
}
