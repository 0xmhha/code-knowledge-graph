package validate

import (
	"context"
	"reflect"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// inferredEdge is a small helper for tests that need many INFERRED
// edges on the same valid src/dst pair.
func inferredEdge(src, dst string, line int) types.Edge {
	return types.Edge{
		Src: src, Dst: dst, Type: types.EdgeCalls,
		Line: line, FilePath: "f.go", Count: 1,
		Confidence: types.ConfInferred,
	}
}

func TestLLMValidator_DryRunEmitsPromptsAsInfoIssues(t *testing.T) {
	g := &graph.Graph{
		Nodes: []types.Node{
			validNode("a", "pkg.A", types.NodeFunction),
			validNode("b", "pkg.B", types.NodeFunction),
		},
		Edges: []types.Edge{inferredEdge("a", "b", 10)},
	}
	v := NewLLMValidator()
	r, err := v.Validate(context.Background(), g, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.Issues) == 0 {
		t.Fatalf("expected at least one prompt issue, got 0")
	}
	got := r.Issues[0]
	if got.Severity != SeverityInfo {
		t.Errorf("severity=%q want %q", got.Severity, SeverityInfo)
	}
	if got.Code != "llm-prompt-dry-run" {
		t.Errorf("code=%q want llm-prompt-dry-run", got.Code)
	}
	if got.Message == "" {
		t.Errorf("expected non-empty Question in Message; got empty")
	}
	if got.EdgeKey == "" {
		t.Errorf("expected EdgeKey on edge-plausibility prompt; got empty")
	}
}

func TestLLMValidator_NoDuplicateOfSchemaFindings(t *testing.T) {
	// Empty-id node — exactly the kind of finding SchemaValidator owns.
	g := &graph.Graph{
		Nodes: []types.Node{
			{ID: "", Type: types.NodeFunction, Confidence: types.ConfExtracted},
		},
	}
	v := NewLLMValidator()
	r, _ := v.Validate(context.Background(), g, nil)
	for _, iss := range r.Issues {
		if iss.Code == "empty-id" || iss.Code == "empty-name" || iss.Code == "empty-qname" {
			t.Errorf("LLMValidator emitted SchemaValidator code %q (duplicate finding)", iss.Code)
		}
	}
}

func TestLLMValidator_RespectsMaxPrompts(t *testing.T) {
	nodes := []types.Node{
		validNode("a", "pkg.A", types.NodeFunction),
		validNode("b", "pkg.B", types.NodeFunction),
	}
	edges := make([]types.Edge, 0, 20)
	for i := 0; i < 20; i++ {
		edges = append(edges, inferredEdge("a", "b", 10+i))
	}
	g := &graph.Graph{Nodes: nodes, Edges: edges}
	v := NewLLMValidator()
	v.MaxPrompts = 3
	r, _ := v.Validate(context.Background(), g, nil)
	if len(r.Issues) != 3 {
		t.Errorf("MaxPrompts=3 yielded %d issues, want 3", len(r.Issues))
	}
}

func TestLLMValidator_DeterministicSampling(t *testing.T) {
	nodes := []types.Node{
		validNode("a", "pkg.A", types.NodeFunction),
		validNode("b", "pkg.B", types.NodeFunction),
		validNode("c", "pkg.C", types.NodeFunction),
	}
	edges := []types.Edge{
		inferredEdge("a", "b", 5),
		inferredEdge("b", "c", 7),
		inferredEdge("c", "a", 9),
		inferredEdge("a", "c", 11),
		inferredEdge("b", "a", 13),
	}
	g := &graph.Graph{Nodes: nodes, Edges: edges}

	v1 := NewLLMValidator()
	v1.MaxPrompts = 3
	r1, _ := v1.Validate(context.Background(), g, nil)

	v2 := NewLLMValidator()
	v2.MaxPrompts = 3
	r2, _ := v2.Validate(context.Background(), g, nil)

	if !reflect.DeepEqual(r1.Issues, r2.Issues) {
		t.Errorf("non-deterministic sampling:\nrun1=%+v\nrun2=%+v", r1.Issues, r2.Issues)
	}
}

func TestLLMValidator_NotDryRunStillNoNetwork(t *testing.T) {
	// Set Endpoint to a non-routable address so that any accidental
	// HTTP attempt would surface as an error within the test timeout.
	// We assert structurally: exactly one Error issue with the
	// "llm-not-yet-wired" code, and no Info prompts.
	g := &graph.Graph{
		Nodes: []types.Node{
			validNode("a", "pkg.A", types.NodeFunction),
			validNode("b", "pkg.B", types.NodeFunction),
		},
		Edges: []types.Edge{inferredEdge("a", "b", 10)},
	}
	v := &LLMValidator{
		DryRun:     false,
		MaxPrompts: 10,
		Endpoint:   "http://192.0.2.1/should-never-be-called",
		Model:      "ignored",
	}
	r, err := v.Validate(context.Background(), g, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.Issues) != 1 {
		t.Fatalf("non-dry-run mode should emit exactly 1 issue, got %d: %+v",
			len(r.Issues), r.Issues)
	}
	got := r.Issues[0]
	if got.Severity != SeverityError {
		t.Errorf("severity=%q want %q", got.Severity, SeverityError)
	}
	if got.Code != "llm-not-yet-wired" {
		t.Errorf("code=%q want llm-not-yet-wired", got.Code)
	}
}

func TestLLMValidator_SparseImplementsSampler(t *testing.T) {
	// Interface "I" with 0 implements edges, imported by 3 distinct files.
	nodes := []types.Node{
		validNode("i", "pkg.I", types.NodeInterface),
		validNode("f1", "pkg.f1", types.NodeFile),
		validNode("f2", "pkg.f2", types.NodeFile),
		validNode("f3", "pkg.f3", types.NodeFile),
	}
	edges := []types.Edge{
		{Src: "f1", Dst: "i", Type: types.EdgeImports, FilePath: "x.go", Count: 1, Confidence: types.ConfExtracted},
		{Src: "f2", Dst: "i", Type: types.EdgeImports, FilePath: "y.go", Count: 1, Confidence: types.ConfExtracted},
		{Src: "f3", Dst: "i", Type: types.EdgeImports, FilePath: "z.go", Count: 1, Confidence: types.ConfExtracted},
	}
	g := &graph.Graph{Nodes: nodes, Edges: edges}
	v := NewLLMValidator()
	r, _ := v.Validate(context.Background(), g, nil)
	found := false
	for _, iss := range r.Issues {
		if iss.NodeID == "pkg.I" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected sparse-subgraph prompt for pkg.I, got %+v", r.Issues)
	}
}
