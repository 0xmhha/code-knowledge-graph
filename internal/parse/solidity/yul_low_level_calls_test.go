package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W10 V2 — Yul-level low-level call receiver resolution.
//
// V0 / V1.1 surfaced Yul ops via markers; V2 closes the dispatch
// loop by parsing the target argument of yul `delegatecall` /
// `call` / `staticcall` as a yul_path, mapping its leading
// identifier to a Sol scope receiver, and emitting EdgeInvokes
// ConfAmbiguous with DispatchKind="low_level_call" (same as
// W7.1's Sol-level shape).
//
// The fixture stages a Proxy contract whose `impl` state-var is
// IImpl-typed. Two Yul callers (delegatecall and staticcall)
// reference `impl` as the target. Each should produce one
// EdgeInvokes from the enclosing function to IImpl.

func TestYulLowLevelCalls_StateVarReceiver(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/yul_receiver", "yul_delegate.sol")

	byID := map[string]types.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}

	type call struct{ src, dst string }
	var got []call
	for _, e := range edges {
		if e.Type != types.EdgeInvokes {
			continue
		}
		if e.DispatchKind != "low_level_call" {
			continue
		}
		got = append(got, call{
			src: byID[e.Src].QualifiedName,
			dst: byID[e.Dst].QualifiedName,
		})
		if e.Confidence != types.ConfAmbiguous {
			t.Errorf("W10 V2 expected ConfAmbiguous, got %q for %s → %s",
				e.Confidence, byID[e.Src].QualifiedName, byID[e.Dst].QualifiedName)
		}
	}

	want := map[call]bool{
		{src: "Proxy.delegate", dst: "IImpl"}: true,
		{src: "Proxy.readImpl", dst: "IImpl"}: true,
	}
	gotSet := map[call]bool{}
	for _, g := range got {
		gotSet[g] = true
	}
	for w := range want {
		if !gotSet[w] {
			t.Errorf("W10 V2 missing edge: %+v (got=%v)", w, got)
		}
	}
}
