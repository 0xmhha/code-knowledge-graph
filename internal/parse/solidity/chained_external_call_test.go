package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W10 V7 — depth-2 chained shape `a().b().call(...)`. Pass 2
// walks two funcReturnTypes hops: a's return type identifies a
// contract, b is looked up on that contract, b's first return
// type must be address-like for the marker to fire.
func TestChainedExternalCall_DeepChainAddressMarker(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/yul_receiver", "sol_deep_chained_external.sol")

	var got bool
	for _, n := range nodes {
		if n.QualifiedName == "Caller.relay" && n.Type == types.NodeFunction {
			got = n.HasExternalCall
			break
		}
	}
	if !got {
		t.Errorf("HasExternalCall on Caller.relay: got false, want true (depth-2 chain to address-return)")
	}
}

// W-C W10 V6 — chained-call receiver shape lights up
// HasExternalCall. `getTarget().call(data)` resolves the inner
// function's first return type via funcReturnTypes during Pass 2;
// when the type is address / address payable, the marker fires.
func TestChainedExternalCall_AddressReturnMarker(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/yul_receiver", "sol_chained_external.sol")

	want := map[string]bool{
		"ChainCaller.relay":     true,  // getTarget() returns address
		"ChainCaller.noopChain": false, // no .call() at all
		"ChainCaller.getTarget": false, // returns address but no call
		"ChainCaller.getInt":    false,
	}
	got := map[string]bool{}
	for _, n := range nodes {
		if n.Type != types.NodeFunction {
			continue
		}
		if _, ok := want[n.QualifiedName]; ok {
			got[n.QualifiedName] = n.HasExternalCall
		}
	}
	for qn, w := range want {
		if got[qn] != w {
			t.Errorf("HasExternalCall on %q: got %v want %v", qn, got[qn], w)
		}
	}
}
