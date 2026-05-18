package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V2.19 — free-function form partial-recovery boundary lock.
//
// V2.6 covered the 2-entry shape; V2.19 extends to 4 entries to
// verify the V0 query's incidental capture survives larger
// free-function alias lists. V2.17's AST probe noted the recovery
// is a fortuitous partial parse rather than first-class grammar
// support — this test pins the broader surface so a future grammar
// bump that flips behavior produces a clearly failing assertion
// rather than a silent semantic shift.
//
// Expected post-V2.6 / pre-grammar-bump behavior:
//   - 1 EdgeUsesFor: Calc → Math (V0 query captures the leading
//     identifier from the partial-recovery `type_alias` child once
//     per (src, dst) regardless of how many free-function aliases
//     reference Math).

func TestUsingForV2190_FreeFunctionFourEntries(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v2190", "free_func_extended.sol")

	byID := map[string]types.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}

	type uf struct{ src, dst string }
	got := map[uf]int{}
	for _, e := range edges {
		if e.Type != types.EdgeUsesFor {
			continue
		}
		got[uf{src: byID[e.Src].Name, dst: byID[e.Dst].Name}]++
	}

	want := map[uf]int{
		{src: "Calc", dst: "Math"}: 1,
	}
	if len(got) != len(want) {
		t.Errorf("V2.19 EdgeUsesFor count: got %d, want %d (got=%v)", len(got), len(want), got)
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("V2.19 missing or wrong count %+v: got %d, want %d", k, got[k], n)
		}
	}

	// Surround-safety: all 4 library functions + Math + Calc + compute index.
	wantNodes := map[string]bool{
		"Math":         false,
		"Math.add":     false,
		"Math.sub":     false,
		"Math.mul":     false,
		"Math.div":     false,
		"Calc":         false,
		"Calc.compute": false,
	}
	for _, n := range nodes {
		if _, ok := wantNodes[n.QualifiedName]; ok {
			wantNodes[n.QualifiedName] = true
		}
	}
	for qn, seen := range wantNodes {
		if !seen {
			t.Errorf("V2.19 surround-safety: %q not indexed", qn)
		}
	}
}
