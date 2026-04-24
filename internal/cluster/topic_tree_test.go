package cluster_test

import (
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/cluster"
	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

func TestTopicTreeMultiResolution(t *testing.T) {
	// Build a tiny graph with two obvious clusters.
	mk := func(id, qname string) types.Node {
		return types.Node{ID: id, Type: types.NodeFunction, Name: qname, QualifiedName: qname,
			FilePath: "f.go", StartLine: 1, EndLine: 1, EndByte: 1,
			Language: "go", Confidence: types.ConfExtracted}
	}
	mke := func(s, d string) types.Edge {
		return types.Edge{Src: s, Dst: d, Type: types.EdgeCalls, Count: 1, Confidence: types.ConfExtracted}
	}
	g := &graph.Graph{
		Nodes: []types.Node{
			mk("a1", "consensus.validateOne"),
			mk("a2", "consensus.validateTwo"),
			mk("a3", "consensus.AuthorizeSigner"),
			mk("b1", "txpool.addLocal"),
			mk("b2", "txpool.addRemote"),
			mk("b3", "txpool.lookup"),
		},
		Edges: []types.Edge{
			mke("a1", "a2"), mke("a2", "a3"), mke("a3", "a1"),
			mke("b1", "b2"), mke("b2", "b3"), mke("b3", "b1"),
			mke("a1", "b1"), // weak bridge
		},
	}
	tt := cluster.BuildTopicTree(g, []float64{0.5, 1.0, 2.0}, 42)
	if n := len(tt.Resolutions); n != 3 {
		t.Fatalf("Resolutions count = %d, want 3", n)
	}
	for i, r := range tt.Resolutions {
		if len(r.Communities) == 0 {
			t.Errorf("resolution %d: 0 communities", i)
		}
		for _, c := range r.Communities {
			if c.Label == "" {
				t.Errorf("resolution %d: empty topic_label", i)
			}
		}
	}
	// At γ=1.0, expect at least one community whose label contains "validate".
	got := false
	for _, c := range tt.Resolutions[1].Communities {
		if strings.Contains(c.Label, "validate") {
			got = true
		}
	}
	if !got {
		t.Errorf("expected at least one γ=1.0 community label containing 'validate'")
	}
}
