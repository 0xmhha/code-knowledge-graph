package solidity_test

import (
	"sort"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V2.20 — operator-form using directive recovery walker.
//
// Three scopes (contract / library / interface) each carry one
// `using {Math.add as +} for uint256;` directive. Before V2.20 all
// three produced 0 EdgeUsesFor because the grammar misparses the
// directive as a state_variable_declaration with no surrounding
// using_directive node (V2.17 AST evidence). V2.20 pattern-matches
// the misparse and emits the binding pair so each container ends up
// with one EdgeUsesFor to the Math library.
//
// V2.7 / V2.14 IOp / V2.17 locks all flip from 0 → 1 in the same
// session; the interface-scope IOp emit in V2.14 also flips.

func TestUsingForV2200_OperatorFormRecovery(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v2200", "operator_form_recovery.sol")

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
		got = append(got, uf{
			src: byID[e.Src].Name,
			dst: byID[e.Dst].Name,
		})
	}
	sort.Slice(got, func(i, j int) bool {
		if got[i].src != got[j].src {
			return got[i].src < got[j].src
		}
		return got[i].dst < got[j].dst
	})

	want := []uf{
		{src: "CContract", dst: "Math"},
		{src: "CInterface", dst: "Math"},
		{src: "CLibrary", dst: "Math"},
	}
	if len(got) != len(want) {
		t.Errorf("V2.20 EdgeUsesFor count: got %d want %d (got=%v)", len(got), len(want), got)
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("V2.20 EdgeUsesFor[%d]: got %+v, want %+v", i, got[i], want[i])
		}
	}

	// Surround-safety: every named declaration still indexes.
	wantNodes := []string{
		"Math", "Math.add",
		"CContract", "CContract.f",
		"CLibrary", "CLibrary.f",
		"CInterface", "CInterface.f",
	}
	seen := map[string]bool{}
	for _, n := range nodes {
		seen[n.QualifiedName] = true
	}
	for _, qn := range wantNodes {
		if !seen[qn] {
			t.Errorf("V2.20 surround-safety: %q not indexed", qn)
		}
	}
}
