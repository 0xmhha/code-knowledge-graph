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
