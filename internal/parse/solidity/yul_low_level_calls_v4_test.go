package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W10 V4 — HasExternalCall marker fires on callables that
// perform a low-level call whose receiver resolves to an address-
// typed Sol scope variable rather than a Contract / Interface. The
// fixture stages a Forwarder contract with two functions: one
// passes `sload(0)` as the yul call target (no Sol identifier, no
// mark), and one passes a Sol-scope local `address t` (marker
// fires).
func TestYulLowLevelCalls_AddressTypedReceiverMarker(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/yul_receiver", "yul_address.sol")

	want := map[string]bool{
		"Forwarder.forward":         false, // sload(0) yul receiver, no Sol scope match
		"Forwarder.forwardViaState": true,  // local `address t` typed receiver
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
		g, present := got[qn]
		if !present {
			t.Errorf("missing NodeFunction %q", qn)
			continue
		}
		if g != w {
			t.Errorf("HasExternalCall on %q: got %v, want %v", qn, g, w)
		}
	}

	// Sanity: both still light up HasLowLevelCall (W10 V3 marker
	// is unconditional for every yul delegatecall/call/staticcall).
	for _, n := range nodes {
		if n.QualifiedName == "Forwarder.forward" || n.QualifiedName == "Forwarder.forwardViaState" {
			if !n.HasLowLevelCall {
				t.Errorf("HasLowLevelCall on %q: got false, want true", n.QualifiedName)
			}
		}
	}
}
