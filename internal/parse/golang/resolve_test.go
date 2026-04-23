package golang_test

import (
	"testing"

	gop "github.com/0xmhha/code-knowledge-graph/internal/parse/golang"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

func TestResolveCrossFileCall(t *testing.T) {
	root := "testdata/resolve"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	var srcID, dstID string
	for _, n := range g.Nodes {
		if n.QualifiedName == "b.Hello" {
			srcID = n.ID
		}
		if n.QualifiedName == "a.Greet" {
			dstID = n.ID
		}
	}
	if srcID == "" || dstID == "" {
		t.Fatalf("missing nodes: srcID=%q dstID=%q", srcID, dstID)
	}
	found := false
	for _, e := range g.Edges {
		if e.Type == types.EdgeCalls && e.Src == srcID && e.Dst == dstID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected edge b.Hello -calls-> a.Greet")
	}
}
