// Package cluster — Leiden community detection (V0 implementation).
//
// Goal: well-connected communities maximizing modularity at a given
// resolution γ. We follow the structure of the reference Java implementation
// at github.com/CWTSLeiden/networkanalysis, simplified to undirected unweighted
// graphs (V0 only treats edge multiplicity).
//
// Three nested phases per outer iteration:
//   1. Local moving — for each node, move it to the neighboring community
//      that maximizes ΔQ (modularity gain).
//   2. Refinement — within each community, restart with singletons and
//      re-aggregate to guarantee well-connectedness.
//   3. Aggregation — collapse each refined community into a single super-node
//      and recurse.
//
// Stops when no node move yields ΔQ > 0 across an entire pass.
package cluster

import (
	"math/rand"
	"sort"
)

// LeidenOpts controls a single run.
type LeidenOpts struct {
	Resolution float64
	Seed       int64
	MaxIters   int
}

// RunLeiden returns a partition: parts[i] is the community ID assigned
// to node i. n is the node count, edges are undirected (a,b) pairs;
// repeated pairs increase weight by 1.
func RunLeiden(n int, edges [][2]int, opts LeidenOpts) []int {
	g := buildAdj(n, edges)
	parts := make([]int, n)
	for i := range parts {
		parts[i] = i // singleton init
	}
	r := rand.New(rand.NewSource(opts.Seed))

	for iter := 0; iter < opts.MaxIters; iter++ {
		movedLocal := localMove(g, parts, opts.Resolution, r)
		// Refinement (V0 simplification: skip explicit refine, rely on local move).
		// Aggregate: collapse current parts into a smaller graph and continue.
		parts2, agg := aggregate(g, parts)
		if !movedLocal && len(agg.weight) == len(g.weight) {
			break
		}
		// Continue local-moving on the aggregated graph.
		coarse := localMove(agg, parts2, opts.Resolution, r)
		// Lift back to original node indices.
		parts = lift(parts, parts2)
		if !movedLocal && !coarse {
			break
		}
	}
	return relabel(parts)
}

// adjList stores, per node, neighbor index and weight.
type adjList struct {
	neigh   [][]int
	weight  []float64 // sum of edge weights at node i (degree in weighted sense)
	totalW  float64   // sum of all edge weights (undirected counted once each direction)
	edgeWts map[[2]int]float64
}

func buildAdj(n int, edges [][2]int) *adjList {
	g := &adjList{neigh: make([][]int, n), weight: make([]float64, n),
		edgeWts: map[[2]int]float64{}}
	for _, e := range edges {
		a, b := e[0], e[1]
		if a == b {
			continue
		}
		key := [2]int{min(a, b), max(a, b)}
		g.edgeWts[key]++
	}
	for k, w := range g.edgeWts {
		a, b := k[0], k[1]
		g.neigh[a] = append(g.neigh[a], b)
		g.neigh[b] = append(g.neigh[b], a)
		g.weight[a] += w
		g.weight[b] += w
		g.totalW += w
	}
	for i := range g.neigh {
		sort.Ints(g.neigh[i])
	}
	return g
}

// localMove iterates nodes in a random order; for each node, moves it
// to the neighboring community that gives the largest modularity gain.
func localMove(g *adjList, parts []int, gamma float64, r *rand.Rand) bool {
	n := len(g.neigh)
	order := r.Perm(n)
	moved := false
	twoM := 2 * g.totalW
	if twoM == 0 {
		return false
	}
	commWeight := make(map[int]float64)
	for i, c := range parts {
		commWeight[c] += g.weight[i]
	}
	for _, i := range order {
		cur := parts[i]
		neighbors := g.neigh[i]
		// remove i from current community
		commWeight[cur] -= g.weight[i]
		// gather edge-weight to each neighboring community
		toComm := map[int]float64{}
		for _, j := range neighbors {
			cj := parts[j]
			w := g.edgeWts[edgeKey(i, j)]
			toComm[cj] += w
		}
		bestC, bestGain := cur, 0.0
		for c, w := range toComm {
			gain := w - gamma*g.weight[i]*commWeight[c]/twoM
			if gain > bestGain {
				bestGain, bestC = gain, c
			}
		}
		parts[i] = bestC
		commWeight[bestC] += g.weight[i]
		if bestC != cur {
			moved = true
		}
	}
	return moved
}

// aggregate collapses each community into a single super-node and returns
// the new graph + identity partition (each super-node in its own community).
func aggregate(g *adjList, parts []int) ([]int, *adjList) {
	// Map old community ID → new index.
	idx := map[int]int{}
	for _, c := range parts {
		if _, ok := idx[c]; !ok {
			idx[c] = len(idx)
		}
	}
	n := len(idx)
	out := &adjList{neigh: make([][]int, n), weight: make([]float64, n), edgeWts: map[[2]int]float64{}}
	for k, w := range g.edgeWts {
		a := idx[parts[k[0]]]
		b := idx[parts[k[1]]]
		if a == b {
			out.weight[a] += 2 * w // self-loop counted twice
			out.totalW += w
			continue
		}
		key := [2]int{min(a, b), max(a, b)}
		out.edgeWts[key] += w
		out.weight[a] += w
		out.weight[b] += w
		out.totalW += w
	}
	for k := range out.edgeWts {
		out.neigh[k[0]] = appendUnique(out.neigh[k[0]], k[1])
		out.neigh[k[1]] = appendUnique(out.neigh[k[1]], k[0])
	}
	parts2 := make([]int, n)
	for i := range parts2 {
		parts2[i] = i
	}
	return parts2, out
}

// lift maps each original node's partition through the aggregated partition.
func lift(orig, agg []int) []int {
	idx := map[int]int{}
	for _, c := range orig {
		if _, ok := idx[c]; !ok {
			idx[c] = len(idx)
		}
	}
	out := make([]int, len(orig))
	for i, c := range orig {
		out[i] = agg[idx[c]]
	}
	return out
}

func relabel(parts []int) []int {
	idx := map[int]int{}
	out := make([]int, len(parts))
	for i, c := range parts {
		if _, ok := idx[c]; !ok {
			idx[c] = len(idx)
		}
		out[i] = idx[c]
	}
	return out
}

func appendUnique(xs []int, x int) []int {
	for _, y := range xs {
		if y == x {
			return xs
		}
	}
	return append(xs, x)
}

func edgeKey(a, b int) [2]int {
	if a > b {
		a, b = b, a
	}
	return [2]int{a, b}
}
