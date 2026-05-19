// Package hunkmodifies computes the H2 line-overlap `modifies`
// edges that connect git Hunk nodes to the code nodes they touch.
//
// Previously this logic lived in `internal/buildpipe`, which made
// it unreachable from packages outside the repository tree. The
// evidence integration test (`pkg/evidence/sol_real_git_test.go`)
// needed the same algorithm and ended up duplicating it inline.
// Promoting the helper to a public package eliminates the drift
// surface and exposes the H2 join to any external consumer that
// has parser output and temporal hunk rows ready.
//
// The package mirrors the design described in
// docs/design/hunk-graph.md §4 — for each NodeHunk, scan
// whitelisted CodeNodes (FunctionLike + TypeLike + Field-ish) in
// the same file and emit an EdgeModifies whenever the
// [StartLine, EndLine] intervals overlap.
package hunkmodifies

import "github.com/0xmhha/code-knowledge-graph/pkg/types"

// NodeWhitelist is the H2 §4.2 "FunctionLike + TypeLike + Field-
// ish" set: only nodes of these kinds receive `modifies` edges
// when a hunk overlaps their byte range. Statement-level kinds
// (CallSite, IfStmt, LoopStmt, ReturnStmt, SwitchStmt) are
// deliberately excluded because they'd explode the edge count by
// ~10x without improving the few-shot retrieval signal that
// motivates the edge.
var NodeWhitelist = map[types.NodeType]bool{
	types.NodeFunction:    true,
	types.NodeMethod:      true,
	types.NodeConstructor: true,
	types.NodeModifier:    true,
	types.NodeStruct:      true,
	types.NodeInterface:   true,
	types.NodeClass:       true,
	types.NodeTypeAlias:   true,
	types.NodeEnum:        true,
	types.NodeContract:    true,
	types.NodeField:       true,
	types.NodeConstant:    true,
	types.NodeVariable:    true,
}

// BuildEdges computes the H2 line-overlap modifies edges for the
// given node set without mutating it. For every NodeHunk it scans
// whitelisted CodeNodes (per NodeWhitelist) in the same file and
// emits an EdgeModifies whenever the [StartLine, EndLine] intervals
// overlap. Edges inherit Confidence from the hunk node — EXTRACTED
// for HEAD-reachable hunks, AMBIGUOUS for the unreachable-history
// track populated by emitUnreachableHunkGraph.
//
// Per design §4.1, candidates iterate in their original emission
// order (file-path bucket preserves insertion); hunks iterate in
// nodes-slice order, so consumers wanting deterministic output
// should sort the hunk subset up front.
//
// Performance characteristic (design §4.3): 700 hunks * ~50
// candidates * O(1) overlap test ~ 35K comparisons, < 50 ms. Real
// monorepos (~9K hunks * ~190 candidates) measure ~1.7M ops,
// still sub-second.
func BuildEdges(nodes []types.Node) []types.Edge {
	byFile := make(map[string][]int, 64)
	for i := range nodes {
		n := &nodes[i]
		if n.Type == types.NodeHunk {
			continue
		}
		if !NodeWhitelist[n.Type] {
			continue
		}
		if n.FilePath == "" {
			continue
		}
		byFile[n.FilePath] = append(byFile[n.FilePath], i)
	}
	if len(byFile) == 0 {
		return nil
	}
	var out []types.Edge
	for i := range nodes {
		hunk := &nodes[i]
		if hunk.Type != types.NodeHunk {
			continue
		}
		candidates, ok := byFile[hunk.FilePath]
		if !ok {
			continue
		}
		hStart, hEnd := hunk.StartLine, hunk.EndLine
		if hEnd < hStart {
			hEnd = hStart
		}
		for _, ci := range candidates {
			cand := &nodes[ci]
			cStart, cEnd := cand.StartLine, cand.EndLine
			if cEnd < cStart {
				cEnd = cStart
			}
			if hStart > cEnd || cStart > hEnd {
				continue
			}
			out = append(out, types.Edge{
				Src:        hunk.ID,
				Dst:        cand.ID,
				Type:       types.EdgeModifies,
				FilePath:   hunk.FilePath,
				Line:       hunk.StartLine,
				Count:      1,
				Confidence: hunk.Confidence,
			})
		}
	}
	return out
}
