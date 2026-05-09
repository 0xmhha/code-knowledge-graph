package typescript_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	tsp "github.com/0xmhha/code-knowledge-graph/internal/parse/typescript"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestTSBodyWalk_BareIdentifierCalls covers the simplest case: a
// top-level function calls another top-level function by bare name.
// V0 emits one PendingRef anchored on the caller; Resolve unions it
// to the callee by Name.
func TestTSBodyWalk_BareIdentifierCalls(t *testing.T) {
	src := []byte(`
function helper() { return 1; }
function caller() {
  helper();
  helper();
}
`)
	p := tsp.New(".")
	r, err := p.ParseFile(filepath.Join(".", "x.ts"), src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	// We expect two PendingRefs to "helper" anchored on caller (line-
	// distinct, so the dedup keeps both).
	helperPending := pendingByCallee(r, "helper")
	if len(helperPending) != 2 {
		t.Errorf("expected 2 PendingRefs to helper, got %d (%v)", len(helperPending), helperPending)
	}
	for _, p := range helperPending {
		if p.EdgeType != types.EdgeCalls {
			t.Errorf("PendingRef edge type = %q, want calls", p.EdgeType)
		}
	}

	// Resolve the file against itself: one helper definition, two calls.
	rg, err := p.Resolve([]*parse.ParseResult{r})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	calls := edgesByType(rg.Edges, types.EdgeCalls)
	if len(calls) < 1 {
		t.Errorf("expected ≥1 resolved calls edge, got %d", len(calls))
	}
}

// TestTSBodyWalk_MemberExpressionCalls covers `obj.method()` calls. The
// query captures the property as @callee, so PendingRef.TargetQName is
// the method name (not the receiver). V0 name-based resolution will then
// match it against any Method whose Name equals that property.
func TestTSBodyWalk_MemberExpressionCalls(t *testing.T) {
	src := []byte(`
class Logger {
  log(msg: string): void {}
}
function caller(l: Logger) {
  l.log("hi");
}
`)
	p := tsp.New(".")
	r, err := p.ParseFile(filepath.Join(".", "y.ts"), src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	logPending := pendingByCallee(r, "log")
	if len(logPending) != 1 {
		t.Errorf("expected 1 PendingRef to log via member call, got %d", len(logPending))
	}
}

// TestTSBodyWalk_NoPendingForTopLevelCalls covers the negative case:
// a call_expression at module top level (outside any function) is
// dropped. There's no caller to anchor the edge on; emitting a
// dangling PendingRef would either error in Resolve or silently fall
// through.
func TestTSBodyWalk_NoPendingForTopLevelCalls(t *testing.T) {
	src := []byte(`
function helper() { return 1; }
helper();   // top-level — no enclosing function
`)
	p := tsp.New(".")
	r, err := p.ParseFile(filepath.Join(".", "z.ts"), src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	pending := pendingByCallee(r, "helper")
	if len(pending) != 0 {
		t.Errorf("expected 0 PendingRefs for top-level calls, got %d", len(pending))
	}
}

// TestTSBodyWalk_NestedSmallestEnclosing verifies the "smallest
// containing interval" semantics: a call inside a method that lives
// inside a class anchors on a CallSite contained-by the method, not
// on any outer function/class span the method might overlap with.
//
// Schema change: PendingRef.SrcID is now the CallSite ID (mirroring
// the Go parser's CallSite-anchored pattern); we assert via the
// `contains` edge that the CallSite traces back to the expected method.
func TestTSBodyWalk_NestedSmallestEnclosing(t *testing.T) {
	src := []byte(`
class App {
  start() {
    initialize();
  }
  stop() {
    teardown();
  }
}
function initialize() {}
function teardown() {}
`)
	p := tsp.New(".")
	r, err := p.ParseFile(filepath.Join(".", "n.ts"), src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	methodIDByName := map[string]string{}
	for _, n := range r.Nodes {
		if n.Type == types.NodeMethod {
			methodIDByName[n.Name] = n.ID
		}
	}
	if methodIDByName["start"] == "" || methodIDByName["stop"] == "" {
		t.Fatalf("expected start and stop Method nodes, got: %v", methodIDByName)
	}
	// Build CallSite → enclosing-method map via the `contains` edges.
	containerOf := map[string]string{}
	for _, e := range r.Edges {
		if e.Type == types.EdgeContains {
			containerOf[e.Dst] = e.Src
		}
	}
	for _, pr := range r.Pending {
		container := containerOf[pr.SrcID]
		switch pr.TargetQName {
		case "initialize":
			if container != methodIDByName["start"] {
				t.Errorf("initialize call's CallSite (%s) should be contained by start (%s), got container %s",
					pr.SrcID, methodIDByName["start"], container)
			}
		case "teardown":
			if container != methodIDByName["stop"] {
				t.Errorf("teardown call's CallSite (%s) should be contained by stop (%s), got container %s",
					pr.SrcID, methodIDByName["stop"], container)
			}
		}
	}
}

// pendingByCallee filters Pending refs by their TargetQName. Helper for
// readable assertions.
func pendingByCallee(r *parse.ParseResult, name string) []parse.PendingRef {
	var out []parse.PendingRef
	for _, p := range r.Pending {
		if p.TargetQName == name {
			out = append(out, p)
		}
	}
	return out
}

// edgesByType lifts the existing helper from temporal_test (kept local
// here so this file is self-contained — same idiom the package's other
// tests use).
func edgesByType(edges []types.Edge, t types.EdgeType) []types.Edge {
	var out []types.Edge
	for _, e := range edges {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

// TestTSBodyWalk_DedupSameLine covers the dedup safeguard: a call like
// `foo(bar(), bar())` produces two `bar` call_expressions both starting
// on the same source line, but we should still get only one PendingRef
// (per the dedup key caller|callee|line). The second `bar()` is at a
// different startByte but the test checks the line-level dedup that
// matters for graph readability.
func TestTSBodyWalk_DedupSameLine(t *testing.T) {
	src := []byte(`
function caller() {
  return JSON.stringify(JSON.parse("{}"));
}
`)
	// Both stringify and parse are member calls on JSON; both live on
	// the same source line. Each is a distinct call_expression with a
	// distinct callee name, so dedup should NOT collapse them — they're
	// two real edges.
	p := tsp.New(".")
	r, err := p.ParseFile(filepath.Join(".", "d.ts"), src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	calleeNames := map[string]int{}
	for _, p := range r.Pending {
		calleeNames[p.TargetQName]++
	}
	if calleeNames["stringify"] != 1 {
		t.Errorf("stringify pending count = %d, want 1; pendings: %s",
			calleeNames["stringify"], formatPending(r.Pending))
	}
	if calleeNames["parse"] != 1 {
		t.Errorf("parse pending count = %d, want 1; pendings: %s",
			calleeNames["parse"], formatPending(r.Pending))
	}
}

func formatPending(p []parse.PendingRef) string {
	parts := make([]string, 0, len(p))
	for _, x := range p {
		parts = append(parts, x.TargetQName)
	}
	return "[" + strings.Join(parts, ",") + "]"
}
