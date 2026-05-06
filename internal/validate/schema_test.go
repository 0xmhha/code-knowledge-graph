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

// TestSchemaValidator_ContextPathBadShape locks down the P2 invariant on
// timeout_path / cancellation_path edges: each must be a self-loop on a
// callable kind (Function/Method). Self-loops on non-callable types
// (e.g. Package) and non-self-loops between callables both fail the shape
// rule. Mirrors TestSchemaValidator_ImplementsBadDst in style.
func TestSchemaValidator_ContextPathBadShape(t *testing.T) {
	g := &graph.Graph{
		Nodes: []types.Node{
			validNode("a", "pkg.A", types.NodeFunction),
			validNode("b", "pkg.B", types.NodeFunction),
			validNode("m", "pkg.T.M", types.NodeMethod),
			validNode("p", "pkg", types.NodePackage),
		},
		Edges: []types.Edge{
			// timeout_path: good self-loop on Function.
			{Src: "a", Dst: "a", Type: types.EdgeTimeoutPath, Confidence: types.ConfExtracted, Line: 1, FilePath: "f.go"},
			// timeout_path: bad — not a self-loop (Function → Function).
			{Src: "a", Dst: "b", Type: types.EdgeTimeoutPath, Confidence: types.ConfExtracted, Line: 2, FilePath: "f.go"},
			// timeout_path: bad — self-loop on a non-callable Package node.
			{Src: "p", Dst: "p", Type: types.EdgeTimeoutPath, Confidence: types.ConfExtracted, Line: 3, FilePath: "f.go"},
			// cancellation_path: good self-loop on Method.
			{Src: "m", Dst: "m", Type: types.EdgeCancellationPath, Confidence: types.ConfExtracted, Line: 4, FilePath: "f.go"},
			// cancellation_path: bad — cross-node between callables.
			{Src: "a", Dst: "b", Type: types.EdgeCancellationPath, Confidence: types.ConfExtracted, Line: 5, FilePath: "f.go"},
			// cancellation_path: bad — self-loop on a Package node.
			{Src: "p", Dst: "p", Type: types.EdgeCancellationPath, Confidence: types.ConfExtracted, Line: 6, FilePath: "f.go"},
		},
	}
	v := NewSchemaValidator()
	r, err := v.Validate(context.Background(), g, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	timeoutBadKeys := map[string]bool{}
	cancelBadKeys := map[string]bool{}
	for _, iss := range r.Issues {
		switch iss.Code {
		case "timeout-path-bad-shape":
			timeoutBadKeys[iss.EdgeKey] = true
		case "cancellation-path-bad-shape":
			cancelBadKeys[iss.EdgeKey] = true
		}
	}
	wantTimeout := map[string]bool{
		"timeout_path:a:b:2": true,
		"timeout_path:p:p:3": true,
	}
	wantCancel := map[string]bool{
		"cancellation_path:a:b:5": true,
		"cancellation_path:p:p:6": true,
	}
	if len(timeoutBadKeys) != 2 {
		t.Errorf("expected 2 timeout-path-bad-shape issues, got %d: %v",
			len(timeoutBadKeys), timeoutBadKeys)
	}
	for k := range wantTimeout {
		if !timeoutBadKeys[k] {
			t.Errorf("missing timeout-path-bad-shape EdgeKey %q in %v", k, timeoutBadKeys)
		}
	}
	if len(cancelBadKeys) != 2 {
		t.Errorf("expected 2 cancellation-path-bad-shape issues, got %d: %v",
			len(cancelBadKeys), cancelBadKeys)
	}
	for k := range wantCancel {
		if !cancelBadKeys[k] {
			t.Errorf("missing cancellation-path-bad-shape EdgeKey %q in %v", k, cancelBadKeys)
		}
	}
	// The good self-loops must NOT have produced bad-shape issues.
	for k := range timeoutBadKeys {
		if k == "timeout_path:a:a:1" {
			t.Errorf("good Function self-loop wrongly flagged timeout-path-bad-shape: %q", k)
		}
	}
	for k := range cancelBadKeys {
		if k == "cancellation_path:m:m:4" {
			t.Errorf("good Method self-loop wrongly flagged cancellation-path-bad-shape: %q", k)
		}
	}
	// The other invariant checks (Implements/ListensOn/Calls) must remain
	// silent for this fixture — no edges of those types are present.
	for _, iss := range r.Issues {
		switch iss.Code {
		case "implements-bad-dst",
			"listens-on-bad-src", "listens-on-bad-dst",
			"calls-bad-src", "calls-bad-dst":
			t.Errorf("unexpected unrelated invariant issue %q: %+v", iss.Code, iss)
		}
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
