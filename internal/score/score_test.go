package score_test

import (
	"math"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/internal/score"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Triangle graph: A -> B -> C -> A
func triangle() *graph.Graph {
	mk := func(id string) types.Node {
		return types.Node{ID: id, Type: types.NodeFunction, Name: id, QualifiedName: id,
			FilePath: "f.go", StartLine: 1, EndLine: 1, EndByte: 1,
			Language: "go", Confidence: types.ConfExtracted}
	}
	a, b, c := mk("aaaaaaaaaaaaaaaa"), mk("bbbbbbbbbbbbbbbb"), mk("cccccccccccccccc")
	mke := func(s, d string) types.Edge {
		return types.Edge{Src: s, Dst: d, Type: types.EdgeCalls, Count: 1, Confidence: types.ConfExtracted}
	}
	return &graph.Graph{
		Nodes: []types.Node{a, b, c},
		Edges: []types.Edge{mke(a.ID, b.ID), mke(b.ID, c.ID), mke(c.ID, a.ID)},
	}
}

func TestDegreeAndUsage(t *testing.T) {
	g := triangle()
	score.Compute(g)
	for _, n := range g.Nodes {
		if n.InDegree != 1 || n.OutDegree != 1 {
			t.Errorf("%s: in=%d out=%d, want 1/1", n.ID, n.InDegree, n.OutDegree)
		}
		if n.UsageScore != 1 {
			t.Errorf("%s: usage=%.2f, want 1", n.ID, n.UsageScore)
		}
	}
}

func TestPageRankSumsToOne(t *testing.T) {
	g := triangle()
	score.Compute(g)
	sum := 0.0
	for _, n := range g.Nodes {
		sum += n.PageRank
	}
	if math.Abs(sum-1.0) > 1e-3 {
		t.Errorf("PageRank sum = %.6f, want ~1.0", sum)
	}
}
