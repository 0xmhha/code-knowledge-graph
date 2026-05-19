package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W10 V3 — Yul let-binding shadow guard + HasLowLevelCall marker
// on the enclosing callable for every Yul delegatecall / call /
// staticcall regardless of receiver resolution.
//
// The fixture stages `delegateShadowed`, where a yul-local
// `let impl := target` shadows the Sol-scope `impl` state-var of
// IImpl type. Without the V3 guard the V2 walker would falsely
// resolve to IImpl; with the guard the emit is skipped. The
// function still carries HasLowLevelCall=true so security tooling
// can still spot the indirection.
func TestYulLowLevelCalls_LetBindingShadow(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/yul_receiver", "yul_shadow.sol")

	byID := map[string]types.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}

	// (a) No EdgeInvokes with DispatchKind=low_level_call from
	// Proxy.delegateShadowed. The shadowed `impl` is a let binding
	// and must not resolve to the Sol state-var of the same name.
	for _, e := range edges {
		if e.Type != types.EdgeInvokes || e.DispatchKind != "low_level_call" {
			continue
		}
		src := byID[e.Src]
		if src.QualifiedName == "Proxy.delegateShadowed" {
			t.Errorf("unexpected shadow leak: %s -> %s line=%d",
				src.QualifiedName, byID[e.Dst].QualifiedName, e.Line)
		}
	}

	// (b) HasLowLevelCall fires on the enclosing callable even
	// though the receiver argument was a yul let-binding.
	var fn types.Node
	for _, n := range nodes {
		if n.QualifiedName == "Proxy.delegateShadowed" && n.Type == types.NodeFunction {
			fn = n
			break
		}
	}
	if fn.ID == "" {
		t.Fatalf("Proxy.delegateShadowed not indexed")
	}
	if !fn.HasLowLevelCall {
		t.Errorf("expected HasLowLevelCall=true on Proxy.delegateShadowed")
	}
}

// W-C W10 V3 marker propagation — every yul delegatecall / call /
// staticcall lights up HasLowLevelCall on the enclosing callable,
// not just the ones where the receiver argument resolves to a Sol
// scope target. Re-uses the existing yul_delegate.sol fixture which
// already exercises the resolved-receiver case.
func TestYulLowLevelCalls_MarkerOnResolvedReceiver(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/yul_receiver", "yul_delegate.sol")

	want := map[string]bool{
		"Proxy.delegate": true,
		"Proxy.readImpl": true,
	}
	got := map[string]bool{}
	for _, n := range nodes {
		if _, ok := want[n.QualifiedName]; ok && n.Type == types.NodeFunction {
			got[n.QualifiedName] = n.HasLowLevelCall
		}
	}
	for qn, w := range want {
		if got[qn] != w {
			t.Errorf("HasLowLevelCall on %q: got %v want %v", qn, got[qn], w)
		}
	}
}
