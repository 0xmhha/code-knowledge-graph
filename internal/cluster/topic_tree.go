package cluster

import (
	"sort"

	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Community is one labeled group within a single resolution.
type Community struct {
	ID      int
	Label   string
	Members []string // node IDs
}

// Resolution captures the partition produced at one γ value.
type Resolution struct {
	Gamma       float64
	Communities []Community
}

// TopicTree holds Leiden communities at multiple resolutions.
type TopicTree struct {
	Resolutions []Resolution
	// For convenience: per-node, the community ID at each resolution.
	NodeToComm []map[string]int // index = resolution index
}

// Adaptive-split thresholds (graphify-inspired — cluster.py:55-58):
//
//   - oversizedFraction:  communities larger than this fraction of the
//     total participant set get re-Leidened. Caps
//     the worst-case "single mega-community contains
//     half the codebase" output.
//   - oversizedMinSize:   absolute lower bound — don't split tiny
//     partitions even if they happen to sit above
//     the fraction (small graphs).
//   - diffuseCohesion:    cohesion < threshold + size >= diffuseMinSize
//     flags a community whose members exist together
//     only because they all touch a doc-hub or test
//     fixture, not because they form a real subsystem.
const (
	oversizedFraction = 0.25
	oversizedMinSize  = 10
	diffuseCohesion   = 0.05
	diffuseMinSize    = 50
)

// BuildTopicTree runs Leiden at each gamma in `gammas`, naming communities,
// then applies an adaptive split pass: communities exceeding
// `oversizedFraction` of the participant set, or low-cohesion communities
// over `diffuseMinSize`, get re-Leidened on their subgraph so the output
// stays readable on graphs where the first Leiden pass collapsed real
// subsystems together.
//
// Meta nodes (Commit, Hunk — schema 1.4/1.8 G6 Temporal) are excluded from
// community participation per hunk-graph.md §11.7 (decision 2026-05-09).
// They have no semantic edges (no calls/invokes/references/etc.) so their
// inclusion would yield singleton communities that pollute the resolution
// without adding signal. Excluded nodes get NO entry in NodeToComm — viewer
// callers must treat absence as "no community" (matches the contract for
// any node the Leiden run drops).
func BuildTopicTree(g *graph.Graph, gammas []float64, seed int64) *TopicTree {
	// Build a compacted index over participating nodes only.
	participants := make([]int, 0, len(g.Nodes))
	idx := make(map[string]int, len(g.Nodes))
	for i, n := range g.Nodes {
		if n.Type == types.NodeCommit || n.Type == types.NodeHunk {
			continue
		}
		idx[n.ID] = len(participants)
		participants = append(participants, i)
	}
	edges := make([][2]int, 0, len(g.Edges))
	for _, e := range g.Edges {
		si, ok := idx[e.Src]
		if !ok {
			continue
		}
		di, ok := idx[e.Dst]
		if !ok {
			continue
		}
		// Only structural edges contribute to community signal at V0
		// (calls, references, uses_type, implements). Filter to keep results stable.
		switch e.Type {
		case types.EdgeCalls, types.EdgeInvokes, types.EdgeReferences,
			types.EdgeUsesType, types.EdgeImplements, types.EdgeExtends:
			edges = append(edges, [2]int{si, di})
		}
	}
	tt := &TopicTree{}
	for _, gamma := range gammas {
		parts := RunLeiden(len(participants), edges, LeidenOpts{
			Resolution: gamma, Seed: seed, MaxIters: 50,
		})
		// Group participant indices by community label. parts[k] is the
		// community ID for participants[k] (i.e. g.Nodes[participants[k]]).
		groups := map[int][]int{}
		for k, c := range parts {
			groups[c] = append(groups[c], k)
		}
		// Adaptive split pass — see splitProblemCommunities doc.
		groups = splitProblemCommunities(groups, edges, len(participants), gamma, seed)
		// Iterate community IDs in sorted order so output is deterministic
		// across map-iteration runs.
		commIDs := make([]int, 0, len(groups))
		for c := range groups {
			commIDs = append(commIDs, c)
		}
		sort.Ints(commIDs)

		nodeMap := map[string]int{}
		var comms []Community
		for _, c := range commIDs {
			members := groups[c]
			// Sort member indices so LabelCommunity sees a deterministic order
			// (topPageRankName falls back to first member when PageRank is unset).
			sort.Ints(members)
			ms := make([]types.Node, 0, len(members))
			ids := make([]string, 0, len(members))
			for _, k := range members {
				ni := participants[k]
				ms = append(ms, g.Nodes[ni])
				ids = append(ids, g.Nodes[ni].ID)
				nodeMap[g.Nodes[ni].ID] = c
			}
			comms = append(comms, Community{
				ID:      c,
				Label:   LabelCommunity(ms),
				Members: ids,
			})
		}
		tt.Resolutions = append(tt.Resolutions, Resolution{
			Gamma:       gamma,
			Communities: comms,
		})
		tt.NodeToComm = append(tt.NodeToComm, nodeMap)
	}
	return tt
}

// splitProblemCommunities runs a second Leiden pass on each community
// flagged as oversized (> oversizedFraction × total) OR diffuse (size ≥
// diffuseMinSize AND cohesion < diffuseCohesion). Returns the post-split
// grouping, indexing fresh splits with new community IDs that don't
// collide with the input keys. Communities that don't trigger either
// rule pass through untouched.
//
// Mirrors graphify cluster.cluster() (cluster.py:96-113) but adapted to
// the integer-indexed participant model CKG already uses for its Leiden
// invocation.
func splitProblemCommunities(groups map[int][]int, edges [][2]int,
	totalNodes int, gamma float64, seed int64) map[int][]int {
	maxAllowedSize := max(int(float64(totalNodes)*oversizedFraction), oversizedMinSize)
	nextID := maxCommunityID(groups) + 1

	out := make(map[int][]int, len(groups))
	for cid, members := range groups {
		oversized := len(members) > maxAllowedSize
		diffuse := len(members) >= diffuseMinSize && cohesionOfMembers(members, edges) < diffuseCohesion
		if !oversized && !diffuse {
			out[cid] = members
			continue
		}
		sub := reLeidenSubgraph(members, edges, gamma, seed)
		if len(sub) <= 1 {
			// Couldn't break it apart further (e.g. fully-connected
			// subgraph or no internal edges) — keep as-is rather than
			// return spurious singletons.
			out[cid] = members
			continue
		}
		// Reassign IDs: first split keeps the original cid for stability;
		// the rest get fresh IDs starting from nextID.
		first := true
		for _, ms := range sub {
			sort.Ints(ms)
			if first {
				out[cid] = ms
				first = false
			} else {
				out[nextID] = ms
				nextID++
			}
		}
	}
	return out
}

// reLeidenSubgraph re-indexes the input member set to a contiguous
// 0..n-1 range, runs Leiden on the induced subgraph, and returns the
// post-partition groupings expressed in the ORIGINAL participant
// indices (so the caller can swap them in without remembering the
// local re-index).
//
// Returns a single group containing all members when no internal
// edges remain (a degenerate case where Leiden has nothing to do),
// signalling the caller to keep the community intact.
func reLeidenSubgraph(members []int, edges [][2]int,
	gamma float64, seed int64) [][]int {
	if len(members) <= 1 {
		return [][]int{members}
	}
	memberSet := make(map[int]bool, len(members))
	for _, m := range members {
		memberSet[m] = true
	}
	localIdx := make(map[int]int, len(members))
	for i, m := range members {
		localIdx[m] = i
	}
	subEdges := make([][2]int, 0, len(edges))
	for _, e := range edges {
		if memberSet[e[0]] && memberSet[e[1]] {
			subEdges = append(subEdges, [2]int{localIdx[e[0]], localIdx[e[1]]})
		}
	}
	if len(subEdges) == 0 {
		return [][]int{members}
	}
	parts := RunLeiden(len(members), subEdges, LeidenOpts{
		Resolution: gamma, Seed: seed, MaxIters: 50,
	})
	subgroups := map[int][]int{}
	for i, c := range parts {
		subgroups[c] = append(subgroups[c], members[i])
	}
	out := make([][]int, 0, len(subgroups))
	for _, ms := range subgroups {
		out = append(out, ms)
	}
	return out
}

// cohesionOfMembers returns the intra-community-edge / max-possible-
// undirected-pair ratio for the given member slice. Mirrors the
// graphify cohesion_score formula and the cmd/ckg/report.go
// computeCohesions function (which operates on string IDs rather than
// integer indices).
func cohesionOfMembers(members []int, edges [][2]int) float64 {
	n := len(members)
	if n <= 1 {
		return 1.0
	}
	memberSet := make(map[int]bool, n)
	for _, m := range members {
		memberSet[m] = true
	}
	intra := 0
	for _, e := range edges {
		if e[0] == e[1] {
			continue
		}
		if memberSet[e[0]] && memberSet[e[1]] {
			intra++
		}
	}
	maxPairs := n * (n - 1) / 2
	if maxPairs == 0 {
		return 0
	}
	return float64(intra) / float64(maxPairs)
}

// maxCommunityID returns the largest community key in groups, or -1 if
// the map is empty. Used to allocate fresh IDs for split children
// without colliding with existing keys.
func maxCommunityID(groups map[int][]int) int {
	maxID := -1
	for c := range groups {
		if c > maxID {
			maxID = c
		}
	}
	return maxID
}
