package cluster_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/cluster"
)

// Two cliques connected by a single bridge edge — Leiden should detect
// exactly two communities at γ=1.0.
func TestLeidenTwoClusters(t *testing.T) {
	// Build edge list. Nodes 0..3 are clique A, 4..7 are clique B,
	// edge (3,4) is the bridge.
	edges := [][2]int{
		{0, 1}, {0, 2}, {0, 3}, {1, 2}, {1, 3}, {2, 3}, // A
		{4, 5}, {4, 6}, {4, 7}, {5, 6}, {5, 7}, {6, 7}, // B
		{3, 4}, // bridge
	}
	parts := cluster.RunLeiden(8, edges, cluster.LeidenOpts{Resolution: 1.0, Seed: 42, MaxIters: 50})
	if got := distinct(parts); got != 2 {
		t.Errorf("Leiden communities = %d, want 2", got)
	}
}

func distinct(p []int) int {
	m := map[int]struct{}{}
	for _, x := range p {
		m[x] = struct{}{}
	}
	return len(m)
}
