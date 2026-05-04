package graph_test

import (
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

func n(id, qname string, t types.NodeType) types.Node {
	return types.Node{ID: id, Type: t, Name: qname, QualifiedName: qname,
		FilePath: "f.go", StartLine: 1, EndLine: 1, StartByte: 0, EndByte: 1,
		Language: "go", Confidence: types.ConfExtracted}
}

// TestBuildEdgeDedup_KeepFirst (G6 v3 § 4.3) — duplicate edges with the same
// (Type, Src, Dst, Line) are deduped keep-first. Tie-breaker is "first wins"
// for Count/Confidence/FilePath, NOT count summation (verified empirically:
// cold builds emit Edge.Count=1 universally, so summing would inflate under
// partial-cache rebuild and break § 7.1 parity).
func TestBuildEdgeDedup_KeepFirst(t *testing.T) {
	a := n("aaaaaaaaaaaaaaaa", "a.A", types.NodeFunction)
	b := n("bbbbbbbbbbbbbbbb", "b.B", types.NodeFunction)
	first := types.Edge{Src: a.ID, Dst: b.ID, Type: types.EdgeCalls,
		Line: 42, Count: 1, Confidence: types.ConfExtracted, FilePath: "first.go"}
	dup := types.Edge{Src: a.ID, Dst: b.ID, Type: types.EdgeCalls,
		Line: 42, Count: 99, Confidence: types.ConfInferred, FilePath: "second.go"}
	noLine := types.Edge{Src: a.ID, Dst: b.ID, Type: types.EdgeCalls,
		Line: 0, Count: 1, Confidence: types.ConfExtracted}

	g, err := graph.Build([]*parse.ResolvedGraph{
		{Nodes: []types.Node{a, b}, Edges: []types.Edge{first, dup, noLine}},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(g.Edges) != 2 {
		t.Fatalf("dedup failed: got %d edges, want 2", len(g.Edges))
	}
	got := g.Edges[0]
	if got.Count != 1 || got.Confidence != types.ConfExtracted || got.FilePath != "first.go" {
		t.Errorf("keep-first violated: got Count=%d Conf=%s FilePath=%q; want first wins",
			got.Count, got.Confidence, got.FilePath)
	}
}

// TestBuildEdgeDedup_DifferentLineKept — line is part of the key, so two
// edges from the same src→dst at different lines must both survive.
func TestBuildEdgeDedup_DifferentLineKept(t *testing.T) {
	a := n("aaaaaaaaaaaaaaaa", "a.A", types.NodeFunction)
	b := n("bbbbbbbbbbbbbbbb", "b.B", types.NodeFunction)
	g, err := graph.Build([]*parse.ResolvedGraph{
		{Nodes: []types.Node{a, b}, Edges: []types.Edge{
			{Src: a.ID, Dst: b.ID, Type: types.EdgeCalls, Line: 10, Count: 1, Confidence: types.ConfExtracted},
			{Src: a.ID, Dst: b.ID, Type: types.EdgeCalls, Line: 20, Count: 1, Confidence: types.ConfExtracted},
		}},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(g.Edges) != 2 {
		t.Errorf("different-line edges merged: got %d, want 2", len(g.Edges))
	}
}

func TestBuildDedupAndValidate(t *testing.T) {
	a := n("aaaaaaaaaaaaaaaa", "a.A", types.NodeFunction)
	b := n("bbbbbbbbbbbbbbbb", "b.B", types.NodeFunction)
	dup := a // same ID
	g, err := graph.Build([]*parse.ResolvedGraph{
		{Nodes: []types.Node{a, b}, Edges: []types.Edge{{Src: a.ID, Dst: b.ID,
			Type: types.EdgeCalls, Count: 1, Confidence: types.ConfExtracted}}},
		{Nodes: []types.Node{dup}, Edges: nil},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(g.Nodes) != 2 {
		t.Errorf("dedup failed: got %d nodes, want 2", len(g.Nodes))
	}

	// Inject a dangling edge and expect Validate to fail.
	g.Edges = append(g.Edges, types.Edge{Src: a.ID, Dst: "ffffffffffffffff",
		Type: types.EdgeCalls, Count: 1, Confidence: types.ConfExtracted})
	err = graph.Validate(g)
	if err == nil || !strings.Contains(err.Error(), "dangling") {
		t.Errorf("Validate should reject dangling edge, got %v", err)
	}
}
