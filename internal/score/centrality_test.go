package score

import (
	"math"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestApproxBetweenness_StarGraph asserts the textbook case: in a
// 5-node star (center connected to 4 leaves) the center has full
// betweenness (every leaf-leaf shortest path traverses it) and the
// leaves have zero. Exact mode (k=0) so the result is deterministic.
func TestApproxBetweenness_StarGraph(t *testing.T) {
	nodes := []types.Node{
		{ID: "c", Type: types.NodeFunction, Confidence: types.ConfExtracted},
		{ID: "l1", Type: types.NodeFunction, Confidence: types.ConfExtracted},
		{ID: "l2", Type: types.NodeFunction, Confidence: types.ConfExtracted},
		{ID: "l3", Type: types.NodeFunction, Confidence: types.ConfExtracted},
		{ID: "l4", Type: types.NodeFunction, Confidence: types.ConfExtracted},
	}
	edges := []types.Edge{
		{Src: "c", Dst: "l1", Type: types.EdgeCalls, Count: 1, Confidence: types.ConfExtracted},
		{Src: "c", Dst: "l2", Type: types.EdgeCalls, Count: 1, Confidence: types.ConfExtracted},
		{Src: "c", Dst: "l3", Type: types.EdgeCalls, Count: 1, Confidence: types.ConfExtracted},
		{Src: "c", Dst: "l4", Type: types.EdgeCalls, Count: 1, Confidence: types.ConfExtracted},
	}
	bc := ApproxBetweenness(nodes, edges, 0, 42)
	// Center: all 6 leaf-leaf paths route through it; normalised value = 1.
	if got := bc["c"]; math.Abs(got-1.0) > 1e-9 {
		t.Errorf("center bc = %f, want 1.0", got)
	}
	for _, leaf := range []string{"l1", "l2", "l3", "l4"} {
		if got := bc[leaf]; got != 0 {
			t.Errorf("leaf %s bc = %f, want 0", leaf, got)
		}
	}
}

// TestApproxBetweenness_PathGraph: in a 5-node path A-B-C-D-E, the middle
// nodes (B, C, D) have positive centrality with C highest.
func TestApproxBetweenness_PathGraph(t *testing.T) {
	nodes := []types.Node{
		{ID: "A", Type: types.NodeFunction, Confidence: types.ConfExtracted},
		{ID: "B", Type: types.NodeFunction, Confidence: types.ConfExtracted},
		{ID: "C", Type: types.NodeFunction, Confidence: types.ConfExtracted},
		{ID: "D", Type: types.NodeFunction, Confidence: types.ConfExtracted},
		{ID: "E", Type: types.NodeFunction, Confidence: types.ConfExtracted},
	}
	edges := []types.Edge{
		{Src: "A", Dst: "B", Type: types.EdgeCalls, Count: 1, Confidence: types.ConfExtracted},
		{Src: "B", Dst: "C", Type: types.EdgeCalls, Count: 1, Confidence: types.ConfExtracted},
		{Src: "C", Dst: "D", Type: types.EdgeCalls, Count: 1, Confidence: types.ConfExtracted},
		{Src: "D", Dst: "E", Type: types.EdgeCalls, Count: 1, Confidence: types.ConfExtracted},
	}
	bc := ApproxBetweenness(nodes, edges, 0, 42)
	if !(bc["C"] > bc["B"] && bc["B"] == bc["D"] && bc["B"] > 0) {
		t.Errorf("expected C highest, B=D>0, A=E=0; got: %v", bc)
	}
	if bc["A"] != 0 || bc["E"] != 0 {
		t.Errorf("endpoints should have 0 bc; got A=%f E=%f", bc["A"], bc["E"])
	}
}

// TestApproxBetweenness_ExcludesMeta verifies that NodeCommit / NodeHunk
// nodes are completely absent from the result map (and that edges to/from
// them don't inflate other nodes' centrality).
func TestApproxBetweenness_ExcludesMeta(t *testing.T) {
	nodes := []types.Node{
		{ID: "fn", Type: types.NodeFunction, Confidence: types.ConfExtracted},
		{ID: "commit", Type: types.NodeCommit, Confidence: types.ConfExtracted},
		{ID: "hunk", Type: types.NodeHunk, Confidence: types.ConfExtracted},
	}
	edges := []types.Edge{
		{Src: "fn", Dst: "commit", Type: types.EdgeChangedIn, Count: 1, Confidence: types.ConfExtracted},
		{Src: "commit", Dst: "hunk", Type: types.EdgeHasHunk, Count: 1, Confidence: types.ConfExtracted},
	}
	bc := ApproxBetweenness(nodes, edges, 0, 42)
	if _, ok := bc["commit"]; ok {
		t.Errorf("commit should not be in betweenness map: %v", bc)
	}
	if _, ok := bc["hunk"]; ok {
		t.Errorf("hunk should not be in betweenness map: %v", bc)
	}
}

// TestApproxBetweenness_SamplingMatchesExact: with sampling k=V the
// result should match exact (k=0).
func TestApproxBetweenness_SamplingMatchesExact(t *testing.T) {
	nodes := []types.Node{
		{ID: "a", Type: types.NodeFunction, Confidence: types.ConfExtracted},
		{ID: "b", Type: types.NodeFunction, Confidence: types.ConfExtracted},
		{ID: "c", Type: types.NodeFunction, Confidence: types.ConfExtracted},
		{ID: "d", Type: types.NodeFunction, Confidence: types.ConfExtracted},
	}
	edges := []types.Edge{
		{Src: "a", Dst: "b", Type: types.EdgeCalls, Count: 1, Confidence: types.ConfExtracted},
		{Src: "b", Dst: "c", Type: types.EdgeCalls, Count: 1, Confidence: types.ConfExtracted},
		{Src: "c", Dst: "d", Type: types.EdgeCalls, Count: 1, Confidence: types.ConfExtracted},
	}
	exact := ApproxBetweenness(nodes, edges, 0, 42)
	sampled := ApproxBetweenness(nodes, edges, 4, 42)
	for id, ev := range exact {
		sv := sampled[id]
		if math.Abs(ev-sv) > 1e-9 {
			t.Errorf("k=V mismatch for %s: exact=%f sampled=%f", id, ev, sv)
		}
	}
}
