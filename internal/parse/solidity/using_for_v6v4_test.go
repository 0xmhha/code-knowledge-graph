package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V4 — namespace-aliased free-function path for the file-
// level operator-form recovery walker.
//
// Sol's `import "./math.sol" as M;` creates the namespace alias
// `M`. A subsequent `using {M.mul as *} for uint256 global;` is
// the dotted-tail-is-the-target shape. Pre-V4 the walker would
// skip the entry at the namespaceAliases guard (no Library named
// `M` to bind against). V4 uses the dotted tail (`mul`) as the
// binding target and the W6 V3 NodeFunction fallback resolves it
// to the cross-file free function.
//
// Expected: 1 EdgeUsesFor (Calc → mul) at the file-level binding
// scope. consumer.sol's Calc fans in this binding via the file-
// level walker; mul lives in math.sol.
func TestUsingForV6V4_NamespaceAliasedFreeFunction(t *testing.T) {
	nodes, edges := parseResolveMultiSol(t, "testdata/using_for_v6v4",
		[]string{"math.sol", "consumer.sol"})

	byID := map[string]types.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	type uf struct{ src, dst string }
	var got []uf
	for _, e := range edges {
		if e.Type != types.EdgeUsesFor {
			continue
		}
		got = append(got, uf{src: byID[e.Src].Name, dst: byID[e.Dst].Name})
	}
	want := uf{src: "Calc", dst: "mul"}
	if len(got) != 1 || got[0] != want {
		t.Errorf("expected one EdgeUsesFor %+v, got %v", want, got)
	}

	wantNodes := []string{"mul", "Calc", "Calc.compute"}
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
