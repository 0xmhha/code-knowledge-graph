// Package score computes per-node graph metrics in place: in/out degree,
// PageRank (damping=0.85, iterations=30), and usage_score (sum of incoming
// "calls"/"invokes" edge counts — used to size super-nodes in the viewer).
package score

import (
	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Compute populates InDegree, OutDegree, PageRank, and UsageScore for each node.
func Compute(g *graph.Graph) {
	idx := make(map[string]int, len(g.Nodes))
	for i, n := range g.Nodes {
		g.Nodes[i].InDegree = 0
		g.Nodes[i].OutDegree = 0
		g.Nodes[i].UsageScore = 0
		idx[n.ID] = i
	}
	for _, e := range g.Edges {
		si, ok := idx[e.Src]
		if !ok {
			continue
		}
		di, ok := idx[e.Dst]
		if !ok {
			continue
		}
		g.Nodes[si].OutDegree += e.Count
		g.Nodes[di].InDegree += e.Count
		if e.Type == types.EdgeCalls || e.Type == types.EdgeInvokes {
			g.Nodes[di].UsageScore += float64(e.Count)
		}
	}
	pageRank(g, 0.85, 30)
}

// pageRank implements the standard iterative algorithm.
func pageRank(g *graph.Graph, damping float64, iters int) {
	n := len(g.Nodes)
	if n == 0 {
		return
	}
	idx := make(map[string]int, n)
	for i, nd := range g.Nodes {
		idx[nd.ID] = i
	}
	out := make([][]int, n)
	outDeg := make([]int, n)
	for _, e := range g.Edges {
		si, ok := idx[e.Src]
		if !ok {
			continue
		}
		di, ok := idx[e.Dst]
		if !ok {
			continue
		}
		out[si] = append(out[si], di)
		outDeg[si]++
	}
	pr := make([]float64, n)
	next := make([]float64, n)
	for i := range pr {
		pr[i] = 1.0 / float64(n)
	}
	teleport := (1 - damping) / float64(n)
	for it := 0; it < iters; it++ {
		for i := range next {
			next[i] = teleport
		}
		dangling := 0.0
		for i := 0; i < n; i++ {
			if outDeg[i] == 0 {
				dangling += pr[i]
				continue
			}
			share := damping * pr[i] / float64(outDeg[i])
			for _, j := range out[i] {
				next[j] += share
			}
		}
		// distribute dangling mass evenly
		add := damping * dangling / float64(n)
		for i := range next {
			next[i] += add
		}
		pr, next = next, pr
	}
	for i := range g.Nodes {
		g.Nodes[i].PageRank = pr[i]
	}
}
