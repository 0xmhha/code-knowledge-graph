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

// edgeKey uniquely identifies an edge by its semantic identity — two edges
// with the same (Type, Src, Dst, Line) describe the same logical relation
// at the same call/reference site. Used by Build for dedup (G6 v3).
type edgeKey struct {
	Type     types.EdgeType
	Src, Dst string
	Line     int
}

// Build merges per-language ResolvedGraphs, deduplicating nodes by ID
// (last-writer wins for attributes — should be identical for true dups)
// and edges by (Type, Src, Dst, Line) keep-first.
//
// Cold-path semantics: each ResolvedGraph comes from a different language,
// so edge keys cannot collide across parts. Dedup is a no-op and Build's
// output is identical to the pre-G6-v3 append-only behaviour.
//
// Partial-cache semantics (G6 v3): the same logical edge can arrive from
// two sources — (a) reloaded from DB for cached files, (b) freshly emitted
// by Pass 2 Resolve over the merged dirty+cached input. Without dedup these
// would double-count in the in-memory Graph that cluster/score/temporal
// passes consume (PageRank seeing 2× weight on cached↔cached edges, etc.).
//
// Tie-breaker is keep-first, NOT count summation, because cold builds
// produce Edge.Count=1 for every edge (verified empirically on go-stablenet:
// 317614 edges all have count=1) — summing would inflate counts under
// partial and break § 7.1 parity with cold.
func Build(parts []*parse.ResolvedGraph) (*Graph, error) {
	byID := make(map[string]types.Node)
	seenEdge := make(map[edgeKey]bool)
	var edges []types.Edge
	for _, p := range parts {
		for _, n := range p.Nodes {
			byID[n.ID] = n
		}
		for _, e := range p.Edges {
			k := edgeKey{e.Type, e.Src, e.Dst, e.Line}
			if seenEdge[k] {
				continue
			}
			seenEdge[k] = true
			edges = append(edges, e)
		}
	}
	nodes := make([]types.Node, 0, len(byID))
	for _, n := range byID {
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return &Graph{Nodes: nodes, Edges: edges}, nil
}
