package graph

import (
	"sort"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Graph is the in-memory CKG graph after build.
type Graph struct {
	Nodes []types.Node
	Edges []types.Edge
}

// Build merges per-language ResolvedGraphs, deduplicating nodes by ID
// (last-writer wins for attributes — should be identical for true dups)
// and concatenating edges. Edges are NOT deduplicated; multiplicity is
// preserved via Edge.Count which the score module aggregates later.
func Build(parts []*parse.ResolvedGraph) (*Graph, error) {
	byID := make(map[string]types.Node)
	var edges []types.Edge
	for _, p := range parts {
		for _, n := range p.Nodes {
			byID[n.ID] = n
		}
		edges = append(edges, p.Edges...)
	}
	nodes := make([]types.Node, 0, len(byID))
	for _, n := range byID {
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return &Graph{Nodes: nodes, Edges: edges}, nil
}
