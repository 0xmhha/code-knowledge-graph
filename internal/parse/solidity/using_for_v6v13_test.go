package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V13 — `using SelfLib for uint256;` declared inside the
// library SelfLib itself. Sol allows a library to reference
// itself as the using-for target; the binding still resolves via
// the standard byName[NodeContract] lookup since libraries are
// indexed as NodeContract + SubKind="library".
//
// V13 locks two properties:
//
//  1. Exactly one EdgeUsesFor is emitted, src == dst == SelfLib.
//     The resolver must not loop or short-circuit when the
//     directive's enclosing scope matches the target.
//  2. The free-function/contract-name collision path
//     (resolveUsingForRef's NodeFunction fallback added in V6 V2.5)
//     must NOT mis-fire for the self-binding case. The fallback
//     is keyed by free-function name, and a library named SelfLib
//     has no free-function counterpart.
func TestUsingForV6V13_LibrarySelfBinding(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v6v13", "self_lib.sol")

	byID := map[string]types.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}

	var usesFor []types.Edge
	for _, e := range edges {
		if e.Type == types.EdgeUsesFor {
			usesFor = append(usesFor, e)
		}
	}
	if len(usesFor) != 1 {
		for _, e := range usesFor {
			t.Logf("EdgeUsesFor: %s -> %s (kind=%q)",
				byID[e.Src].Name, byID[e.Dst].Name, e.DispatchKind)
		}
		t.Fatalf("expected exactly one EdgeUsesFor, got %d", len(usesFor))
	}
	e := usesFor[0]

	srcName := byID[e.Src].Name
	dstName := byID[e.Dst].Name
	if srcName != "SelfLib" || dstName != "SelfLib" {
		t.Errorf("self-binding: got %s -> %s, want SelfLib -> SelfLib", srcName, dstName)
	}

	// Target must be the library NodeContract, not a NodeFunction
	// fallback hit (V6 V2.5's free-function path).
	if byID[e.Dst].Type != types.NodeContract {
		t.Errorf("self-binding target type: got %v, want NodeContract", byID[e.Dst].Type)
	}

	// Surround-safety: SelfLib and SelfLib.ping must still index.
	wantNodes := []string{"SelfLib", "SelfLib.ping"}
	seen := map[string]bool{}
	for _, n := range nodes {
		seen[n.QualifiedName] = true
	}
	for _, qn := range wantNodes {
		if !seen[qn] {
			t.Errorf("surround-safety: %q not indexed", qn)
		}
	}
}
