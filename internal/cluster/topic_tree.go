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
func BuildTopicTree(g *graph.Graph, gammas []float64, seed int64) *TopicTree {
	idx := make(map[string]int, len(g.Nodes))
	for i, n := range g.Nodes {
		idx[n.ID] = i
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
		parts := RunLeiden(len(g.Nodes), edges, LeidenOpts{
			Resolution: gamma, Seed: seed, MaxIters: 50,
		})
		// Group node indices by community label.
		groups := map[int][]int{}
		for i, c := range parts {
			groups[c] = append(groups[c], i)
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
			for _, ni := range members {
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
