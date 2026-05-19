package hunkmodifies

import (
	"sort"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestBuildEdges_CrossLanguageFixture — W-C W11 V5. Verifies that
// BuildEdges correctly emits modifies edges across language
// boundaries when a commit touches files in different languages.
// The fixture stages a synthetic node set with Sol contract +
// fields, TS class + methods, Go functions + structs, plus hunks
// distributed across files. The contract:
//
//   - Each hunk only joins to code nodes in the SAME file (no
//     cross-file leak).
//   - Per-language node-kind variety still resolves correctly
//     (NodeFunction Go/TS, NodeMethod TS, NodeContract Sol,
//     NodeField Sol, NodeStruct Go).
//   - Confidence inherits from the hunk regardless of which
//     language the target lives in.
//
// This locks the API contract that pkg/hunkmodifies makes no
// assumption about the source language — it operates purely on
// (FilePath, StartLine, EndLine, Type) tuples.
func TestBuildEdges_CrossLanguageFixture(t *testing.T) {
	nodes := []types.Node{
		// --- Sol file: contract + two fields + one function ---
		{ID: "solContract", Type: types.NodeContract, FilePath: "contracts/Wallet.sol",
			StartLine: 5, EndLine: 50, Confidence: types.ConfExtracted},
		{ID: "solField1", Type: types.NodeField, FilePath: "contracts/Wallet.sol",
			StartLine: 8, EndLine: 8, Confidence: types.ConfExtracted},
		{ID: "solField2", Type: types.NodeField, FilePath: "contracts/Wallet.sol",
			StartLine: 9, EndLine: 9, Confidence: types.ConfExtracted},
		{ID: "solFn", Type: types.NodeFunction, FilePath: "contracts/Wallet.sol",
			StartLine: 20, EndLine: 30, Confidence: types.ConfExtracted},

		// --- TS file: class with two methods ---
		{ID: "tsClass", Type: types.NodeClass, FilePath: "frontend/wallet.ts",
			StartLine: 1, EndLine: 40, Confidence: types.ConfExtracted},
		{ID: "tsMethod1", Type: types.NodeMethod, FilePath: "frontend/wallet.ts",
			StartLine: 5, EndLine: 15, Confidence: types.ConfExtracted},
		{ID: "tsMethod2", Type: types.NodeMethod, FilePath: "frontend/wallet.ts",
			StartLine: 20, EndLine: 30, Confidence: types.ConfExtracted},

		// --- Go file: struct + function ---
		{ID: "goStruct", Type: types.NodeStruct, FilePath: "backend/wallet.go",
			StartLine: 10, EndLine: 14, Confidence: types.ConfExtracted},
		{ID: "goFn", Type: types.NodeFunction, FilePath: "backend/wallet.go",
			StartLine: 20, EndLine: 35, Confidence: types.ConfExtracted},

		// --- Hunks across all three files ---
		// h1 touches Sol field1 only (overlaps 8..9, hits field1 only)
		{ID: "h1", Type: types.NodeHunk, FilePath: "contracts/Wallet.sol",
			StartLine: 8, EndLine: 8, Confidence: types.ConfExtracted},
		// h2 touches Sol function body
		{ID: "h2", Type: types.NodeHunk, FilePath: "contracts/Wallet.sol",
			StartLine: 25, EndLine: 26, Confidence: types.ConfExtracted},
		// h3 touches TS method1 (5..15)
		{ID: "h3", Type: types.NodeHunk, FilePath: "frontend/wallet.ts",
			StartLine: 10, EndLine: 12, Confidence: types.ConfExtracted},
		// h4 touches Go function body (20..35)
		{ID: "h4", Type: types.NodeHunk, FilePath: "backend/wallet.go",
			StartLine: 25, EndLine: 28, Confidence: types.ConfExtracted},
		// h5 touches a file with no code nodes — drops silently
		{ID: "h5", Type: types.NodeHunk, FilePath: "docs/README.md",
			StartLine: 1, EndLine: 5, Confidence: types.ConfExtracted},
	}

	edges := BuildEdges(nodes)

	// Expected modifies edges. h1 hits solField1 (line 8) AND
	// solContract (5..50 envelope — Contract is in NodeWhitelist).
	// h2 hits solContract AND solFn. h3 hits tsClass AND tsMethod1.
	// h4 hits goFn. h5 drops.
	type pair struct{ src, dst string }
	want := map[pair]bool{
		{src: "h1", dst: "solContract"}: true,
		{src: "h1", dst: "solField1"}:   true,
		{src: "h2", dst: "solContract"}: true,
		{src: "h2", dst: "solFn"}:       true,
		{src: "h3", dst: "tsClass"}:     true,
		{src: "h3", dst: "tsMethod1"}:   true,
		{src: "h4", dst: "goFn"}:        true,
	}
	got := map[pair]bool{}
	for _, e := range edges {
		got[pair{src: e.Src, dst: e.Dst}] = true
	}

	missing := []string{}
	for p := range want {
		if !got[p] {
			missing = append(missing, p.src+"->"+p.dst)
		}
	}
	extra := []string{}
	for p := range got {
		if !want[p] {
			extra = append(extra, p.src+"->"+p.dst)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("missing edges: %v", missing)
	}
	if len(extra) > 0 {
		t.Errorf("unexpected edges: %v", extra)
	}

	// (b) Cross-file leak guard: no hunk's modifies edge crosses
	// into a different file path. Catches a regression where the
	// file bucket index would mistakenly broaden the candidate
	// pool across language boundaries.
	idToFile := map[string]string{}
	for _, n := range nodes {
		idToFile[n.ID] = n.FilePath
	}
	for _, e := range edges {
		if idToFile[e.Src] != idToFile[e.Dst] {
			t.Errorf("cross-file leak: %s (%s) -> %s (%s)",
				e.Src, idToFile[e.Src], e.Dst, idToFile[e.Dst])
		}
	}

	// (c) Confidence inheritance.
	for _, e := range edges {
		if e.Confidence != types.ConfExtracted {
			t.Errorf("edge %s->%s confidence: got %v want EXTRACTED",
				e.Src, e.Dst, e.Confidence)
		}
	}
}

// TestBuildEdges_AmbiguousHunkInheritsConfidence — locks the
// per-edge Confidence behaviour when the hunk is AMBIGUOUS
// (unreachable-history track). The modifies edge inherits the
// hunk's confidence regardless of the target's confidence so
// downstream evidence filtering can drop AMBIGUOUS edges as a set.
func TestBuildEdges_AmbiguousHunkInheritsConfidence(t *testing.T) {
	nodes := []types.Node{
		{ID: "fn", Type: types.NodeFunction, FilePath: "a.go",
			StartLine: 1, EndLine: 5, Confidence: types.ConfExtracted},
		{ID: "h", Type: types.NodeHunk, FilePath: "a.go",
			StartLine: 2, EndLine: 3, Confidence: types.ConfAmbiguous},
	}
	edges := BuildEdges(nodes)
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
	if edges[0].Confidence != types.ConfAmbiguous {
		t.Errorf("edge confidence: got %v want AMBIGUOUS", edges[0].Confidence)
	}
}
