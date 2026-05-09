// Package score — centrality.go provides Brandes betweenness centrality
// with source sampling, used by the GRAPH_REPORT generator to surface
// "bridge nodes": symbols that connect otherwise-distant parts of the
// graph and therefore drive cross-cutting concerns.
//
// Why sampling: full Brandes is O(V·(V+E)), which is ~4.6×10^11 ops on
// the go-stablenet self-graph (220K nodes / 2M edges). Source-sampled
// Brandes (k random sources, default 100) is O(k·(V+E)) — ~200M ops,
// sub-second in Go. Estimates are unbiased after the V/k scale-up
// multiplier, accurate to within ~5% for top-N ranking purposes
// (graphify uses the same approach in nx.betweenness_centrality(k=…)).
//
// Meta nodes (Commit, Hunk — schema 1.4/1.8 G6 Temporal) are excluded
// from both the source pool AND the output map: they have no semantic
// edges that would produce meaningful centrality scores, and including
// them would dilute the V scale factor.
package score

import (
	"math/rand"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// ApproxBetweenness returns per-node betweenness centrality using
// Brandes' algorithm with source sampling. k≤0 or k≥|V| selects every
// node (exact). The graph is treated as undirected — bridge identification
// doesn't depend on direction (same convention graphify and most network-
// analysis libraries follow). Edges from/to meta nodes are dropped before
// the BFS, so e.g. a Function→Commit changed_in edge doesn't inflate the
// Function's centrality through history-only links.
//
// seed pins the source-sampling RNG so the report is deterministic across
// runs against the same graph.
func ApproxBetweenness(nodes []types.Node, edges []types.Edge, k int, seed int64) map[string]float64 {
	// Build the participant index: only non-meta nodes can route flow.
	nodeIdx := make(map[string]int, len(nodes))
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if isMetaNodeType(n.Type) {
			continue
		}
		nodeIdx[n.ID] = len(ids)
		ids = append(ids, n.ID)
	}
	V := len(ids)
	if V == 0 {
		return nil
	}

	// Build undirected adjacency (dedup parallel edges by (min,max) pair).
	adj := make([][]int, V)
	type pair struct{ a, b int }
	seen := make(map[pair]struct{}, len(edges))
	for _, e := range edges {
		si, ok1 := nodeIdx[e.Src]
		di, ok2 := nodeIdx[e.Dst]
		if !ok1 || !ok2 || si == di {
			continue
		}
		p := pair{si, di}
		if si > di {
			p = pair{di, si}
		}
		if _, exists := seen[p]; exists {
			continue
		}
		seen[p] = struct{}{}
		adj[si] = append(adj[si], di)
		adj[di] = append(adj[di], si)
	}

	// Pick sources.
	var sources []int
	if k <= 0 || k >= V {
		sources = make([]int, V)
		for i := range sources {
			sources[i] = i
		}
	} else {
		rng := rand.New(rand.NewSource(seed))
		sources = rng.Perm(V)[:k]
	}

	bc := make([]float64, V)
	sigma := make([]float64, V)
	dist := make([]int, V)
	delta := make([]float64, V)
	pred := make([][]int, V)
	for i := range pred {
		pred[i] = pred[i][:0]
	}

	stack := make([]int, 0, V)
	queue := make([]int, 0, V)

	for _, s := range sources {
		// Reset per-source state in place.
		for i := 0; i < V; i++ {
			sigma[i] = 0
			dist[i] = -1
			delta[i] = 0
			pred[i] = pred[i][:0]
		}
		stack = stack[:0]
		queue = queue[:0]

		sigma[s] = 1
		dist[s] = 0
		queue = append(queue, s)
		for len(queue) > 0 {
			v := queue[0]
			queue = queue[1:]
			stack = append(stack, v)
			for _, w := range adj[v] {
				if dist[w] < 0 {
					dist[w] = dist[v] + 1
					queue = append(queue, w)
				}
				if dist[w] == dist[v]+1 {
					sigma[w] += sigma[v]
					pred[w] = append(pred[w], v)
				}
			}
		}

		// Back-propagation in non-increasing distance from s (which is
		// exactly reverse-BFS-discovery order — Brandes' insight).
		for i := len(stack) - 1; i >= 0; i-- {
			w := stack[i]
			for _, v := range pred[w] {
				if sigma[w] == 0 {
					continue
				}
				delta[v] += (sigma[v] / sigma[w]) * (1 + delta[w])
			}
			if w != s {
				bc[w] += delta[w]
			}
		}
	}

	// Sampling correction: scale up so the estimate is unbiased.
	if len(sources) > 0 && len(sources) < V {
		scale := float64(V) / float64(len(sources))
		for i := range bc {
			bc[i] *= scale
		}
	}
	// Undirected double-count correction. Brandes-from-every-source counts
	// each unordered pair {s,t} twice (once when source=s, once when
	// source=t); halve to recover the unordered-pair sum that the
	// normalisation expects. NetworkX's betweenness_centrality applies the
	// same correction for undirected graphs (rescale=False mode in their
	// source) before the normalisation step below.
	for i := range bc {
		bc[i] /= 2.0
	}
	// Standard undirected normalisation: divide by (V-1)(V-2)/2 — the
	// number of unordered pairs not involving v. Yields values in [0, 1]
	// where 1 = "every shortest path passes through this node" (e.g. the
	// center of a star with V≥3).
	if V > 2 {
		norm := 2.0 / float64(V-1) / float64(V-2)
		for i := range bc {
			bc[i] *= norm
		}
	}

	out := make(map[string]float64, V)
	for i, id := range ids {
		if bc[i] > 0 {
			out[id] = bc[i]
		}
	}
	return out
}
