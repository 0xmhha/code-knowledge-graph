package hunkmodifies

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestBuildEdges_BasicOverlap locks the public API contract on a
// small synthetic dataset. Two hunks in the same file overlap two
// distinct functions; the expected output is two EdgeModifies
// edges with the hunk node as src and the function node as dst.
// Equal-line-range candidates outside the whitelist (e.g.
// NodeFile) drop. Hunks in a file that has no code nodes drop.
func TestBuildEdges_BasicOverlap(t *testing.T) {
	nodes := []types.Node{
		{ID: "fn1", Type: types.NodeFunction, FilePath: "a.sol", StartLine: 10, EndLine: 20, Confidence: types.ConfExtracted},
		{ID: "fn2", Type: types.NodeFunction, FilePath: "a.sol", StartLine: 30, EndLine: 40, Confidence: types.ConfExtracted},
		{ID: "stmt", Type: types.NodeIfStmt, FilePath: "a.sol", StartLine: 12, EndLine: 14, Confidence: types.ConfExtracted},
		{ID: "h1", Type: types.NodeHunk, FilePath: "a.sol", StartLine: 15, EndLine: 18, Confidence: types.ConfExtracted},
		{ID: "h2", Type: types.NodeHunk, FilePath: "a.sol", StartLine: 35, EndLine: 36, Confidence: types.ConfExtracted},
		{ID: "h3", Type: types.NodeHunk, FilePath: "b.sol", StartLine: 1, EndLine: 5, Confidence: types.ConfExtracted}, // no code nodes in b.sol
	}
	edges := BuildEdges(nodes)
	if len(edges) != 2 {
		t.Fatalf("expected 2 EdgeModifies edges, got %d (%v)", len(edges), edges)
	}
	want := map[string]string{
		"h1": "fn1",
		"h2": "fn2",
	}
	got := map[string]string{}
	for _, e := range edges {
		if e.Type != types.EdgeModifies {
			t.Errorf("unexpected edge type: %v", e.Type)
		}
		got[e.Src] = e.Dst
		if e.Confidence != types.ConfExtracted {
			t.Errorf("confidence on %s->%s: got %v want EXTRACTED", e.Src, e.Dst, e.Confidence)
		}
	}
	for src, dst := range want {
		if got[src] != dst {
			t.Errorf("expected %s -> %s, got %s -> %s", src, dst, src, got[src])
		}
	}
}

// TestNodeWhitelist_KnownKinds locks the published whitelist so a
// silent removal of a node type from the set (which would drop
// modifies edges) shows up as a failing test.
func TestNodeWhitelist_KnownKinds(t *testing.T) {
	want := []types.NodeType{
		types.NodeFunction, types.NodeMethod, types.NodeConstructor,
		types.NodeModifier, types.NodeStruct, types.NodeInterface,
		types.NodeClass, types.NodeTypeAlias, types.NodeEnum,
		types.NodeContract, types.NodeField, types.NodeConstant,
		types.NodeVariable,
	}
	for _, kind := range want {
		if !NodeWhitelist[kind] {
			t.Errorf("NodeWhitelist missing %q", kind)
		}
	}
	// Statement-level kinds must stay out.
	for _, kind := range []types.NodeType{
		types.NodeIfStmt, types.NodeLoopStmt, types.NodeCallSite,
		types.NodeReturnStmt, types.NodeSwitchStmt, types.NodeHunk,
	} {
		if NodeWhitelist[kind] {
			t.Errorf("NodeWhitelist unexpectedly contains %q", kind)
		}
	}
}
