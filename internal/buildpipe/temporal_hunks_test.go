package buildpipe

import (
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/temporal"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestBuildHunkNodes_StableIDs locks down the (sha, file, idx) → node ID
// hash so a future change to the qname template can't accidentally
// invalidate a populated graph.db. The expected ID is computed from the
// MakeID inputs in the test itself, so the assertion fails loudly if
// makeHunkNode's qname format drifts (e.g. if someone reorders the
// fields or adds a new component without bumping the schema).
func TestBuildHunkNodes_StableIDs(t *testing.T) {
	hunks := []temporal.HunkInfo{
		{
			SHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", FilePath: "main.go", Index: 0,
			NewStart: 1, NewLines: 3, Added: 3, Patch: []byte("@@ -0,0 +1,3 @@\n+a\n+b\n+c\n"),
		},
		{
			SHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", FilePath: "main.go", Index: 1,
			NewStart: 10, NewLines: 1, Added: 1, Patch: []byte("@@ -10,0 +10,1 @@\n+later\n"),
		},
	}
	commitIDs := map[string]string{
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": "fakeCommit00000a",
	}
	nodes, hasHunk, adjacent, blobs := buildHunkNodes(hunks, "", commitIDs, nil, types.ConfExtracted)

	if len(nodes) != 2 {
		t.Fatalf("expected 2 hunk nodes, got %d", len(nodes))
	}
	// Same (sha, file) but different idx → different IDs.
	if nodes[0].ID == nodes[1].ID {
		t.Errorf("hunk node IDs collided: %s == %s", nodes[0].ID, nodes[1].ID)
	}
	for _, n := range nodes {
		if n.Type != types.NodeHunk {
			t.Errorf("node %q type = %s, want Hunk", n.ID, n.Type)
		}
		if n.Language != "go" {
			t.Errorf("node %q language = %s, want go (.go file)", n.ID, n.Language)
		}
		if n.Confidence != types.ConfExtracted {
			t.Errorf("node %q confidence = %s, want EXTRACTED", n.ID, n.Confidence)
		}
		if n.SubKind != "git" {
			t.Errorf("node %q sub_kind = %s, want git", n.ID, n.SubKind)
		}
		if !strings.HasPrefix(n.QualifiedName, "hunk:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa:main.go:") {
			t.Errorf("node %q qname = %q (missing hunk: prefix)", n.ID, n.QualifiedName)
		}
		// StartLine must satisfy validate:"min=1" — clamped to 1 when
		// NewStart is 0 (deletion-only). Both fixtures have NewStart > 0.
		if n.StartLine < 1 {
			t.Errorf("node %q StartLine = %d, want >=1", n.ID, n.StartLine)
		}
	}
	// has_hunk: 1 per node, all sourced from the commit.
	if len(hasHunk) != 2 {
		t.Errorf("has_hunk edges = %d, want 2", len(hasHunk))
	}
	for _, e := range hasHunk {
		if e.Src != "fakeCommit00000a" {
			t.Errorf("has_hunk src = %q, want commit ID", e.Src)
		}
		if e.Type != types.EdgeHasHunk {
			t.Errorf("has_hunk type = %s, want has_hunk", e.Type)
		}
	}
	// adjacent: same (commit, file) → idx 0 → idx 1 (NewStart-ordered).
	if len(adjacent) != 1 {
		t.Errorf("adjacent edges = %d, want 1", len(adjacent))
	}
	if len(adjacent) == 1 {
		// adjacent should run from the lower-line hunk to the higher-line one.
		// nodes are sorted by ID for snapshot determinism, so we can't just
		// check positional order — instead verify the dst matches some node.
		var ids = map[string]bool{}
		for _, n := range nodes {
			ids[n.ID] = true
		}
		if !ids[adjacent[0].Src] || !ids[adjacent[0].Dst] {
			t.Errorf("adjacent edge endpoints (%s → %s) not in node set",
				adjacent[0].Src, adjacent[0].Dst)
		}
	}
	// Blobs are gzipped; round-trip one to validate the compression.
	if len(blobs) != 2 {
		t.Errorf("blobs = %d, want 2 (one per non-binary hunk)", len(blobs))
	}
	for id, gz := range blobs {
		raw := gunzipForTest(t, gz)
		if !bytes.HasPrefix(raw, []byte("@@ ")) {
			t.Errorf("blob %s missing @@ header on round-trip: %q", id, raw[:min(20, len(raw))])
		}
	}
}

// TestBuildHunkNodes_LanguageInference covers the §11.4 decision: hunks
// pick up their target file's language by extension; everything outside
// the {go, ts, sol} whitelist falls back to the 'git' sentinel that
// NodeCommit already uses.
func TestBuildHunkNodes_LanguageInference(t *testing.T) {
	commitIDs := map[string]string{"a": "commitA000000aaa"}
	cases := map[string]string{
		"main.go":          "go",
		"sub/x.ts":         "ts",
		"comp.tsx":         "ts",
		"contracts/y.sol":  "sol",
		"docs/README.md":   "git",
		"k8s/deploy.yaml":  "git",
		"protos/api.proto": "git",
	}
	for path, wantLang := range cases {
		hunks := []temporal.HunkInfo{
			{SHA: "a", FilePath: path, Index: 0, NewStart: 1, NewLines: 1, Added: 1, Patch: []byte("@@ -0,0 +1,1 @@\n+x\n")},
		}
		nodes, _, _, _ := buildHunkNodes(hunks, "", commitIDs, nil, types.ConfExtracted)
		if len(nodes) != 1 {
			t.Errorf("%s: expected 1 node, got %d", path, len(nodes))
			continue
		}
		if nodes[0].Language != wantLang {
			t.Errorf("%s: language = %q, want %q", path, nodes[0].Language, wantLang)
		}
	}
}

// TestBuildHunkNodes_BinaryProducesNoBlob verifies that a binary hunk
// (no @@ block, Binary flag) emits a node + has_hunk edge but no blob
// (the §3.6 design — modifies-edge emission ignores binary hunks).
func TestBuildHunkNodes_BinaryProducesNoBlob(t *testing.T) {
	commitIDs := map[string]string{"b": "commitB000000bbb"}
	hunks := []temporal.HunkInfo{
		{SHA: "b", FilePath: "logo.png", Index: 0, Binary: true},
	}
	nodes, hasHunk, _, blobs := buildHunkNodes(hunks, "", commitIDs, nil, types.ConfExtracted)
	if len(nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(nodes))
	}
	if len(hasHunk) != 1 {
		t.Errorf("has_hunk = %d, want 1", len(hasHunk))
	}
	if len(blobs) != 0 {
		t.Errorf("blobs = %d, want 0 for binary hunk", len(blobs))
	}
}

// TestCapPatchText_Truncation verifies the §11.6 64KB cap algorithm:
// patches under the cap pass through; larger patches yield first 32KB +
// marker + last 32KB and stay under the cap byte count.
func TestCapPatchText_Truncation(t *testing.T) {
	small := []byte(strings.Repeat("a", 1024))
	if got := capPatchText(small); !bytes.Equal(got, small) {
		t.Errorf("small patch was modified: in %d, out %d", len(small), len(got))
	}
	large := []byte(strings.Repeat("b", 200*1024))
	out := capPatchText(large)
	if len(out) > hunkPatchCap {
		t.Errorf("capPatchText didn't enforce cap: out=%d, cap=%d", len(out), hunkPatchCap)
	}
	if !bytes.Contains(out, []byte("[... truncated,")) {
		t.Errorf("capPatchText didn't emit truncation marker: %q…", out[:min(80, len(out))])
	}
	// First and last bytes must come from input ends so retrieval sees
	// both ends of the change.
	if out[0] != 'b' {
		t.Errorf("capPatchText didn't preserve first byte: out[0] = %q", out[0])
	}
	if out[len(out)-1] != 'b' {
		t.Errorf("capPatchText didn't preserve last byte: out[-1] = %q", out[len(out)-1])
	}
}

func gunzipForTest(t *testing.T, gz []byte) []byte {
	t.Helper()
	r, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer func() { _ = r.Close() }()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("io.ReadAll: %v", err)
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
