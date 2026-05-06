package validate

import (
	"context"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

func validNode(id, qname string, t types.NodeType) types.Node {
	return types.Node{
		ID: id, Type: t, Name: qname, QualifiedName: qname,
		FilePath: "f.go", StartLine: 1, EndLine: 1,
		Language: "go", Confidence: types.ConfExtracted,
	}
}

func TestSchemaValidator_HappyPath(t *testing.T) {
	g := &graph.Graph{
		Nodes: []types.Node{
			validNode("a", "pkg.A", types.NodeFunction),
			validNode("b", "pkg.B", types.NodeFunction),
		},
		Edges: []types.Edge{
			{Src: "a", Dst: "b", Type: types.EdgeCalls, Confidence: types.ConfExtracted, Line: 10, FilePath: "f.go"},
		},
	}
	v := NewSchemaValidator()
	r, err := v.Validate(context.Background(), g, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.HasErrors() {
		t.Errorf("happy path produced errors: %+v", r.Issues)
	}
}

func TestSchemaValidator_EmptyFields(t *testing.T) {
	g := &graph.Graph{
		Nodes: []types.Node{
			{ID: "a", Type: types.NodeFunction, Confidence: types.ConfExtracted}, // missing name+qname+filepath
		},
	}
	v := NewSchemaValidator()
	r, _ := v.Validate(context.Background(), g, nil)
	codes := map[string]bool{}
	for _, iss := range r.Issues {
		codes[iss.Code] = true
	}
	for _, want := range []string{"empty-name", "empty-qname", "empty-file-path"} {
		if !codes[want] {
			t.Errorf("missing expected issue code %q in %v", want, codes)
		}
	}
}

func TestSchemaValidator_DanglingEdge(t *testing.T) {
	g := &graph.Graph{
		Nodes: []types.Node{validNode("a", "pkg.A", types.NodeFunction)},
		Edges: []types.Edge{
			{Src: "a", Dst: "ghost", Type: types.EdgeCalls, Confidence: types.ConfExtracted},
		},
	}
	v := NewSchemaValidator()
	r, _ := v.Validate(context.Background(), g, nil)
	found := false
	for _, iss := range r.Issues {
		if iss.Code == "dangling-dst" && strings.Contains(iss.EdgeKey, "ghost") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected dangling-dst issue, got %+v", r.Issues)
	}
}

func TestSchemaValidator_ImplementsBadDst(t *testing.T) {
	g := &graph.Graph{
		Nodes: []types.Node{
			validNode("s", "pkg.S", types.NodeStruct),
			validNode("f", "pkg.F", types.NodeFunction),
		},
		Edges: []types.Edge{
			// implements onto a Function (wrong) instead of Interface
			{Src: "s", Dst: "f", Type: types.EdgeImplements, Confidence: types.ConfExtracted},
		},
	}
	v := NewSchemaValidator()
	r, _ := v.Validate(context.Background(), g, nil)
	found := false
	for _, iss := range r.Issues {
		if iss.Code == "implements-bad-dst" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected implements-bad-dst issue, got %+v", r.Issues)
	}
}
