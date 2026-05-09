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

// BuildTopicTree runs Leiden at each gamma in `gammas`, naming communities.
// Used to populate the topic_tree SQLite table downstream.
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
