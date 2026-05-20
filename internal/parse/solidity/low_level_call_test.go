package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W7.1 V0 — low-level call dispatch.
//
// target.call(...) / .delegatecall(...) / .staticcall(...) are workhorse
// primitives in proxy / upgradeable / bytes-encoded dispatch patterns and
// W3 explicitly excluded them (dispatch.go §0). V0 closes the gap: emit
// one EdgeInvokes per call, DispatchKind="low_level_call", ConfAmbiguous.
//
// V0 receiver resolution chain mirrors W6 lookupReceiverType (state-var
// → param → local-var); type → byName[NodeContract|NodeInterface] →
// Dst. Unresolved (no NodeContract/Interface with that type) → drop.
// `address(x)` cast receivers drop in V0 per W7-D2.
//
// Tests below verify the 3 primitives × state-var receiver baseline.

// TestLowLevelCall_StateVarReceiver — `target.call(data)`,
// `target.delegatecall(data)`, `target.staticcall(data)` where `target`
// is a state-var of interface type IFoo. Each emits 1 EdgeInvokes
// (Function → IFoo) at ConfAmbiguous with DispatchKind="low_level_call".
func TestLowLevelCall_StateVarReceiver(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/low_level_call", "state_var_receiver.sol")

	byID := map[string]types.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}

	// (a) Count EdgeInvokes whose DispatchKind == "low_level_call".
	type call struct{ src, dst, kind string }
	var got []call
	for _, e := range edges {
		if e.Type != types.EdgeInvokes {
			continue
		}
		if e.DispatchKind != "low_level_call" {
			continue
		}
		got = append(got, call{
			src:  byID[e.Src].QualifiedName,
			dst:  byID[e.Dst].QualifiedName,
			kind: e.DispatchKind,
		})
		// (b) Confidence must be AMBIGUOUS regardless of file boundary.
		if e.Confidence != types.ConfAmbiguous {
			t.Errorf("W7.1 expected ConfAmbiguous, got %q for %s → %s",
				e.Confidence, byID[e.Src].QualifiedName, byID[e.Dst].QualifiedName)
		}
	}

	want := []call{
		{src: "Proxy.viaCall", dst: "IFoo", kind: "low_level_call"},
		{src: "Proxy.viaDelegatecall", dst: "IFoo", kind: "low_level_call"},
		{src: "Proxy.viaStaticcall", dst: "IFoo", kind: "low_level_call"},
	}

	if len(got) != len(want) {
		t.Errorf("W7.1 EdgeInvokes count: got %d, want %d\n got=%v\nwant=%v",
			len(got), len(want), got, want)
		return
	}
	// Compare unordered (resolver order may vary).
	matched := map[string]bool{}
	for _, g := range got {
		matched[g.src+"|"+g.dst+"|"+g.kind] = true
	}
	for _, w := range want {
		if !matched[w.src+"|"+w.dst+"|"+w.kind] {
			t.Errorf("W7.1 missing edge: %+v", w)
		}
	}

	// (c) Surround-safety: IFoo, IFoo.bar, Proxy, 3 functions index.
	wantNodes := map[string]bool{
		"IFoo":                  false,
		"IFoo.bar":              false,
		"Proxy":                 false,
		"Proxy.viaCall":         false,
		"Proxy.viaDelegatecall": false,
		"Proxy.viaStaticcall":   false,
	}
	for _, n := range nodes {
		if _, ok := wantNodes[n.QualifiedName]; ok {
			wantNodes[n.QualifiedName] = true
		}
	}
	for qn, seen := range wantNodes {
		if !seen {
			t.Errorf("W7.1 surround-safety: %q not indexed", qn)
		}
	}
}
