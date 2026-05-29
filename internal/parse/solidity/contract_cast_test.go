package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W8 V0 — contract-type cast dispatch.
//
// `MyContract(addr).method()` is the concrete-contract sibling of
// `IInterface(addr).method()` (W3). The AST shape is identical;
// only the resolver target index differs (byName[NodeContract]
// instead of byName[NodeInterface]).
//
// V0 scope (mirrors W3 / W7.1 conventions):
//   - One EdgeInvokes per matched call_expression.
//   - DispatchKind = "contract_cast".
//   - Confidence = ConfAmbiguous (runtime address determines the
//     real target, same as W3).
//   - Unresolved type name (no NodeContract with that name) drops
//     the edge.
//   - When the cast target is also a known Interface, W3 emits
//     instead; the two walkers don't double-emit because they query
//     disjoint name indices (NodeContract vs NodeInterface).

func TestContractCast_Dispatch(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/contract_cast", "contract_type_cast.sol")

	byID := map[string]types.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}

	for _, e := range edges {
		if e.Type != types.EdgeInvokes {
			continue
		}
		if e.DispatchKind != "contract_cast" {
			continue
		}
		if e.Confidence != types.ConfAmbiguous {
			t.Errorf("W8 expected ConfAmbiguous, got %q for %s",
				e.Confidence, byID[e.Src].QualifiedName)
		}
	}

	// Both calls in Caller.forward target Vault.deposit / Vault.withdraw.
	// V0 resolves at TypeName level so the Dst is the function node
	// (Vault.deposit or Vault.withdraw); the test asserts (Src, Dst).
	type pair struct{ src, dst string }
	gotPairs := map[pair]int{}
	for _, e := range edges {
		if e.Type != types.EdgeInvokes || e.DispatchKind != "contract_cast" {
			continue
		}
		gotPairs[pair{
			src: byID[e.Src].QualifiedName,
			dst: byID[e.Dst].QualifiedName,
		}]++
	}

	wantPairs := map[pair]int{
		{src: "Caller.forward", dst: "Vault.deposit"}:  1,
		{src: "Caller.forward", dst: "Vault.withdraw"}: 1,
	}
	if len(gotPairs) != len(wantPairs) {
		t.Errorf("W8 EdgeInvokes(contract_cast) pair count: got %d, want %d (got=%v)",
			len(gotPairs), len(wantPairs), gotPairs)
	}
	for p, n := range wantPairs {
		if gotPairs[p] != n {
			t.Errorf("W8 missing pair %+v (want count %d, got %d)", p, n, gotPairs[p])
		}
	}

	// Surround-safety: every declaration indexes.
	wantNodes := map[string]bool{
		"Vault":          false,
		"Vault.deposit":  false,
		"Vault.withdraw": false,
		"Caller":         false,
		"Caller.forward": false,
	}
	for _, n := range nodes {
		if _, ok := wantNodes[n.QualifiedName]; ok {
			wantNodes[n.QualifiedName] = true
		}
	}
	for qn, seen := range wantNodes {
		if !seen {
			t.Errorf("W8 surround-safety: %q not indexed", qn)
		}
	}
}
