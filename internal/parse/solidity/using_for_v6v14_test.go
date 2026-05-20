package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V14 — transitive using-for audit. A library Outer
// declares `using Inner for uint256;` inside its own scope; the
// contract Caller declares `using Outer for uint256;` at its
// file/contract scope. V14 locks two contracts:
//
//  1. Two distinct EdgeUsesFor edges are emitted with no
//     duplication and no cross-pollination:
//     - Outer  -> Inner
//     - Caller -> Outer
//  2. Both targets resolve as NodeContract (libraries are stored
//     as NodeContract + SubKind="library" — the V11/V13 lookup
//     paths apply uniformly).
//
// A regression that confuses the enclosing scope of the inner
// using directive (e.g. attributing it to the file rather than
// the library body) would either collapse the two edges into
// one or attribute Outer's directive to Caller, both of which
// fail this lock.
func TestUsingForV6V14_TransitiveLibraryChain(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v6v14", "transitive.sol")

	byID := map[string]types.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}

	type edgeKey struct{ src, dst string }
	got := map[edgeKey]int{}
	for _, e := range edges {
		if e.Type != types.EdgeUsesFor {
			continue
		}
		k := edgeKey{byID[e.Src].Name, byID[e.Dst].Name}
		got[k]++
		if byID[e.Dst].Type != types.NodeContract {
			t.Errorf("EdgeUsesFor dst %q: type=%v, want NodeContract", byID[e.Dst].Name, byID[e.Dst].Type)
		}
	}

	want := map[edgeKey]int{
		{"Outer", "Inner"}:  1,
		{"Caller", "Outer"}: 1,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("EdgeUsesFor %s -> %s: count %d, want %d", k.src, k.dst, got[k], w)
		}
	}
	// Surface any extra EdgeUsesFor that shouldn't exist (e.g.
	// Caller -> Inner from a transitive collapse).
	for k, c := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected EdgeUsesFor %s -> %s (count %d)", k.src, k.dst, c)
		}
	}
}
