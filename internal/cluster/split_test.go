package cluster

import "testing"

// TestSplitProblemCommunities_OversizedTriggers verifies that a single
// community covering > 25% of nodes triggers the re-Leiden split when
// the subgraph has internal structure to partition on.
func TestSplitProblemCommunities_OversizedTriggers(t *testing.T) {
	// 100-node "community" that's actually two disconnected groups of 50,
	// linked by zero edges between them — Leiden re-pass should split.
	members := make([]int, 100)
	for i := range members {
		members[i] = i
	}
	groups := map[int][]int{0: members}
	edges := [][2]int{}
	for i := 0; i < 49; i++ {
		edges = append(edges, [2]int{i, i + 1})
	}
	for i := 50; i < 99; i++ {
		edges = append(edges, [2]int{i, i + 1})
	}
	out := splitProblemCommunities(groups, edges, 100, 1.0, 42)
	if len(out) < 2 {
		t.Errorf("oversized community with two disconnected halves should split, got %d groups", len(out))
	}
	total := 0
	for _, ms := range out {
		total += len(ms)
	}
	if total != 100 {
		t.Errorf("split total = %d, want 100 (no member loss)", total)
	}
}

// TestSplitProblemCommunities_DiffuseTriggers covers the cohesion-based
// trigger: a community with very few internal edges (cohesion far below
// 0.05) should be split even when not oversized.
func TestSplitProblemCommunities_DiffuseTriggers(t *testing.T) {
	members := make([]int, 60)
	for i := range members {
		members[i] = i
	}
	groups := map[int][]int{0: members}
	// Two paths (size 28 + 30) with no inter-clique edges and a few
	// stragglers — the whole bag has cohesion well below 0.05.
	edges := [][2]int{}
	for i := 0; i < 27; i++ {
		edges = append(edges, [2]int{i, i + 1})
	}
	for i := 30; i < 59; i++ {
		edges = append(edges, [2]int{i, i + 1})
	}
	// totalNodes large enough that the 25% size rule does NOT trigger
	// (we want only the cohesion rule to fire).
	out := splitProblemCommunities(groups, edges, 1000, 1.0, 42)
	if len(out) < 2 {
		t.Errorf("diffuse community should split via cohesion trigger, got %d groups", len(out))
	}
}

// TestCohesionOfMembers_KnownValues sanity-checks the cohesion formula
// against hand-computed values: 4-clique → 1.0, triangle missing one
// edge → ~0.667, isolated pair → 0.0.
func TestCohesionOfMembers_KnownValues(t *testing.T) {
	clique := []int{0, 1, 2, 3}
	cliqueEdges := [][2]int{
		{0, 1}, {0, 2}, {0, 3}, {1, 2}, {1, 3}, {2, 3},
	}
	if got := cohesionOfMembers(clique, cliqueEdges); got != 1.0 {
		t.Errorf("4-clique cohesion = %v, want 1.0", got)
	}
	tri := []int{0, 1, 2}
	triEdges := [][2]int{{0, 1}, {0, 2}}
	if got := cohesionOfMembers(tri, triEdges); got <= 0.6 || got >= 0.7 {
		t.Errorf("triangle-1-edge cohesion = %v, want ~0.667", got)
	}
	disconnected := []int{0, 1}
	if got := cohesionOfMembers(disconnected, [][2]int{}); got != 0 {
		t.Errorf("disconnected-pair cohesion = %v, want 0.0", got)
	}
}
