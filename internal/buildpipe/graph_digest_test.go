package buildpipe

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

func codeNodes() []types.Node {
	return []types.Node{
		{ID: "aaaaaaaaaaaaaaaa", Type: types.NodeFunction, CanonicalID: "pkg.F", QualifiedName: "pkg.F", FilePath: "a.go", StartLine: 1, EndLine: 3},
		{ID: "bbbbbbbbbbbbbbbb", Type: types.NodeStruct, CanonicalID: "pkg.T", QualifiedName: "pkg.T", FilePath: "a.go", StartLine: 5, EndLine: 9},
	}
}

func codeEdges() []types.Edge {
	return []types.Edge{
		{Type: types.EdgeCalls, Src: "aaaaaaaaaaaaaaaa", Dst: "bbbbbbbbbbbbbbbb", Line: 2},
	}
}

// TestComputeGraphDigest_Deterministic locks the core contract: the digest is
// independent of node/edge slice order (ADR-0002 / Q1).
func TestComputeGraphDigest_Deterministic(t *testing.T) {
	n := codeNodes()
	e := codeEdges()
	d1 := ComputeGraphDigest(n, e)

	// Reversed input order must yield the identical digest.
	nRev := []types.Node{n[1], n[0]}
	d2 := ComputeGraphDigest(nRev, e)
	if d1 != d2 {
		t.Errorf("digest depends on node order: %s vs %s", d1, d2)
	}
	if d1 == "" || len(d1) != 64 {
		t.Errorf("digest not a sha256 hex: %q", d1)
	}
}

// TestComputeGraphDigest_ExcludesDerivedMetrics guards that pagerank/degrees do
// NOT affect the digest — otherwise incremental (which recomputes them) would
// diverge from cold for the same logical graph.
func TestComputeGraphDigest_ExcludesDerivedMetrics(t *testing.T) {
	base := codeNodes()
	withMetrics := codeNodes()
	withMetrics[0].PageRank = 0.42
	withMetrics[0].InDegree = 7
	withMetrics[0].OutDegree = 3
	withMetrics[0].UsageScore = 9.9
	withMetrics[1].Complexity = 12

	if ComputeGraphDigest(base, codeEdges()) != ComputeGraphDigest(withMetrics, codeEdges()) {
		t.Error("derived metrics (pagerank/degrees/usage/complexity) changed the digest")
	}
}

// TestComputeGraphDigest_ExcludesTemporal guards that Commit/Hunk nodes and
// temporal edges are ignored — a temporal-only rebuild must leave the digest
// unchanged (prevents CKV false-positive asserts).
func TestComputeGraphDigest_ExcludesTemporal(t *testing.T) {
	base := ComputeGraphDigest(codeNodes(), codeEdges())

	nodesPlusTemporal := append(codeNodes(),
		types.Node{ID: "cccccccccccccccc", Type: types.NodeCommit, QualifiedName: "commit:abc", FilePath: "", StartLine: 1, EndLine: 1},
		types.Node{ID: "dddddddddddddddd", Type: types.NodeHunk, QualifiedName: "hunk:1", FilePath: "a.go", StartLine: 1, EndLine: 1},
	)
	edgesPlusTemporal := append(codeEdges(),
		types.Edge{Type: types.EdgeChangedIn, Src: "aaaaaaaaaaaaaaaa", Dst: "cccccccccccccccc", Line: 0},
		types.Edge{Type: types.EdgeHasHunk, Src: "cccccccccccccccc", Dst: "dddddddddddddddd", Line: 0},
	)
	if got := ComputeGraphDigest(nodesPlusTemporal, edgesPlusTemporal); got != base {
		t.Errorf("temporal nodes/edges changed the digest: %s vs %s", got, base)
	}
}

// TestComputeGraphDigest_SensitiveToIdentity confirms the digest DOES change
// when a real identity field (canonical_id) changes — it is not degenerate.
func TestComputeGraphDigest_SensitiveToIdentity(t *testing.T) {
	base := ComputeGraphDigest(codeNodes(), codeEdges())
	mutated := codeNodes()
	mutated[0].CanonicalID = "pkg.F2"
	if ComputeGraphDigest(mutated, codeEdges()) == base {
		t.Error("digest did not change when canonical_id changed")
	}
}
