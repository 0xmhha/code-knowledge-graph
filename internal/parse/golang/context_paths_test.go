package golang_test

import (
	"os"
	"path/filepath"
	"testing"

	gop "github.com/0xmhha/code-knowledge-graph/internal/parse/golang"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// context_paths_test.go locks down the P2 detector contract: every
// recognised context.With* call site produces exactly one self-loop edge
// of the right type and confidence. Run via LoadAndResolve so we exercise
// the typed (EXTRACTED) path and the same wiring buildpipe uses.

// findContextEdgesFor returns timeout_path / cancellation_path edges whose
// Src points at a Function node with the given qualified name. Uses qname
// because the fixture's package is `context_paths_fixture` so the qname is
// stable: `context_paths_fixture.<FuncName>`.
func findContextEdgesFor(t *testing.T, edges []types.Edge, nodes []types.Node, funcQname string) []types.Edge {
	t.Helper()
	var funcID string
	for i := range nodes {
		n := &nodes[i]
		if n.Type == types.NodeFunction && n.QualifiedName == funcQname {
			funcID = n.ID
			break
		}
	}
	if funcID == "" {
		t.Fatalf("Function %q not found in graph nodes", funcQname)
	}
	out := []types.Edge{}
	for _, e := range edges {
		if e.Src != funcID {
			continue
		}
		if e.Type != types.EdgeTimeoutPath && e.Type != types.EdgeCancellationPath {
			continue
		}
		out = append(out, e)
	}
	return out
}

// TestContextPaths_Timeout — WithTimeoutOnly produces exactly one
// timeout_path self-loop.
func TestContextPaths_Timeout(t *testing.T) {
	root := "testdata/context_paths"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	got := findContextEdgesFor(t, g.Edges, g.Nodes, "context_paths_fixture.WithTimeoutOnly")
	if len(got) != 1 {
		t.Fatalf("WithTimeoutOnly edges: got %d, want 1 — %+v", len(got), got)
	}
	if got[0].Type != types.EdgeTimeoutPath {
		t.Errorf("edge type = %q, want %q", got[0].Type, types.EdgeTimeoutPath)
	}
	if got[0].Src != got[0].Dst {
		t.Errorf("expected self-loop, got src=%s dst=%s", got[0].Src, got[0].Dst)
	}
}

// TestContextPaths_Deadline — WithDeadlineOnly produces a timeout_path edge
// (deadline is treated as a timeout variant per emitContextPaths comment).
func TestContextPaths_Deadline(t *testing.T) {
	root := "testdata/context_paths"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	got := findContextEdgesFor(t, g.Edges, g.Nodes, "context_paths_fixture.WithDeadlineOnly")
	if len(got) != 1 {
		t.Fatalf("WithDeadlineOnly edges: got %d, want 1 — %+v", len(got), got)
	}
	if got[0].Type != types.EdgeTimeoutPath {
		t.Errorf("edge type = %q, want %q (deadline rolls into timeout_path)",
			got[0].Type, types.EdgeTimeoutPath)
	}
}

// TestContextPaths_Cancel — WithCancelOnly produces a cancellation_path
// self-loop.
func TestContextPaths_Cancel(t *testing.T) {
	root := "testdata/context_paths"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	got := findContextEdgesFor(t, g.Edges, g.Nodes, "context_paths_fixture.WithCancelOnly")
	if len(got) != 1 {
		t.Fatalf("WithCancelOnly edges: got %d, want 1 — %+v", len(got), got)
	}
	if got[0].Type != types.EdgeCancellationPath {
		t.Errorf("edge type = %q, want %q", got[0].Type, types.EdgeCancellationPath)
	}
	if got[0].Src != got[0].Dst {
		t.Errorf("expected self-loop, got src=%s dst=%s", got[0].Src, got[0].Dst)
	}
}

// TestContextPaths_CancelCause — WithCancelCauseSite produces exactly one
// cancellation_path self-loop in typed mode (EXTRACTED). Locks down the
// Go 1.20+ context.WithCancelCause variant alongside WithCancel.
func TestContextPaths_CancelCause(t *testing.T) {
	root := "testdata/context_paths"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	got := findContextEdgesFor(t, g.Edges, g.Nodes, "context_paths_fixture.WithCancelCauseSite")
	if len(got) != 1 {
		t.Fatalf("WithCancelCauseSite edges: got %d, want 1 — %+v", len(got), got)
	}
	if got[0].Type != types.EdgeCancellationPath {
		t.Errorf("edge type = %q, want %q", got[0].Type, types.EdgeCancellationPath)
	}
	if got[0].Src != got[0].Dst {
		t.Errorf("expected self-loop, got src=%s dst=%s", got[0].Src, got[0].Dst)
	}
	if got[0].Confidence != types.ConfExtracted {
		t.Errorf("typed-mode emit must be EXTRACTED, got %q", got[0].Confidence)
	}
}

// TestContextPaths_TwoSites — TwoTimeoutSites produces TWO timeout_path
// edges with distinct Lines (graph.Build's 4-tuple dedup keeps them
// separate when Line differs).
func TestContextPaths_TwoSites(t *testing.T) {
	root := "testdata/context_paths"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	got := findContextEdgesFor(t, g.Edges, g.Nodes, "context_paths_fixture.TwoTimeoutSites")
	if len(got) != 2 {
		t.Fatalf("TwoTimeoutSites edges: got %d, want 2 — %+v", len(got), got)
	}
	if got[0].Type != types.EdgeTimeoutPath || got[1].Type != types.EdgeTimeoutPath {
		t.Errorf("both edges must be timeout_path; got %q and %q",
			got[0].Type, got[1].Type)
	}
	if got[0].Line == got[1].Line {
		t.Errorf("expected distinct Lines, both at line %d", got[0].Line)
	}
}

// TestContextPaths_NoneForBareFunction — NoContextOps produces zero
// timeout/cancellation edges. Guards against the detector firing on
// any function it visits.
func TestContextPaths_NoneForBareFunction(t *testing.T) {
	root := "testdata/context_paths"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	got := findContextEdgesFor(t, g.Edges, g.Nodes, "context_paths_fixture.NoContextOps")
	if len(got) != 0 {
		t.Errorf("NoContextOps must have zero context-path edges, got %d: %+v",
			len(got), got)
	}
}

// TestContextPaths_SelfLoopShape — every emitted timeout_path /
// cancellation_path edge must have Src == Dst (self-loop contract). This
// is the invariant the SchemaValidator's bad-shape rules also enforce —
// regression here fails fast at parse time.
func TestContextPaths_SelfLoopShape(t *testing.T) {
	root := "testdata/context_paths"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	count := 0
	for _, e := range g.Edges {
		if e.Type != types.EdgeTimeoutPath && e.Type != types.EdgeCancellationPath {
			continue
		}
		count++
		if e.Src != e.Dst {
			t.Errorf("context-path edge is not a self-loop: %+v", e)
		}
	}
	if count == 0 {
		t.Error("fixture must produce at least one context-path edge")
	}
}

// TestContextPaths_ConfidenceWithTypes — when LoadAndResolve is used
// (typed mode, typesInfo non-nil), every emitted context-path edge has
// EXTRACTED confidence. Regression check that the typed branch in
// classifyContextCall is reached for all four constructor names.
func TestContextPaths_ConfidenceWithTypes(t *testing.T) {
	root := "testdata/context_paths"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	for _, e := range g.Edges {
		if e.Type != types.EdgeTimeoutPath && e.Type != types.EdgeCancellationPath {
			continue
		}
		if e.Confidence != types.ConfExtracted {
			t.Errorf("typed-mode emit must be EXTRACTED, got %q for edge %+v",
				e.Confidence, e)
		}
	}
}

// TestContextPaths_ASTOnlyFallback — when the parser is in AST-only mode
// (ParseFile called without SetPackages), the detector still fires on
// `context.WithTimeout(...)` selectors but emits INFERRED. Documents the
// false-positive trade-off of the AST-only path.
func TestContextPaths_ASTOnlyFallback(t *testing.T) {
	dir := "testdata/context_paths"
	src, err := os.ReadFile(filepath.Join(dir, "fixture.go"))
	if err != nil {
		t.Fatal(err)
	}
	p := gop.New(dir) // no SetPackages → AST-only
	res, err := p.ParseFile(filepath.Join(dir, "fixture.go"), src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	gotTimeout := 0
	gotCancel := 0
	for _, e := range res.Edges {
		switch e.Type {
		case types.EdgeTimeoutPath:
			gotTimeout++
			if e.Confidence != types.ConfInferred {
				t.Errorf("AST-only timeout_path confidence = %q, want INFERRED",
					e.Confidence)
			}
		case types.EdgeCancellationPath:
			gotCancel++
			if e.Confidence != types.ConfInferred {
				t.Errorf("AST-only cancellation_path confidence = %q, want INFERRED",
					e.Confidence)
			}
		}
	}
	// Fixture has 4 timeout sites (1 in WithTimeoutOnly + 1 in WithDeadlineOnly
	// + 2 in TwoTimeoutSites) and 2 cancellation sites (WithCancelOnly +
	// WithCancelCauseSite).
	if gotTimeout != 4 {
		t.Errorf("AST-only timeout_path count = %d, want 4", gotTimeout)
	}
	if gotCancel != 2 {
		t.Errorf("AST-only cancellation_path count = %d, want 2", gotCancel)
	}
}
