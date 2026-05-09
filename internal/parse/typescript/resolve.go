package typescript

import (
	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Resolve unions per-file results and emits cross-file edges from
// PendingRef queues. V0 cross-file resolution is name-based — TS has
// no in-process type system the way Go does (Track C used go/packages),
// so we lean on three signals to keep the precision/recall trade-off
// honest:
//
//  1. **Caller-file locality**. If the candidate set contains a node
//     in the SAME file as the caller, prefer those — TS scoping rules
//     mean a same-file `Foo()` is overwhelmingly the local Foo.
//
//  2. **Confidence reflects ambiguity**.
//      - exactly one candidate (after the locality filter) → INFERRED.
//      - 2+ candidates → AMBIGUOUS, picking the highest-PageRank as
//        the dst so the edge still points somewhere reasonable, but
//        flagged so downstream consumers can de-rank or re-review.
//
//  3. **No cross-axis pollution**. Only Function / Method / Class
//     definitions populate the byName index — Imports / Decorators /
//     Enum members never become call targets even when the callee
//     name accidentally matches one. (graphify's
//     `_cross_language` downgrade tackles the same false-positive
//     class for its multi-language extractor; CKG's TS pass is
//     single-language so we don't need that specific filter, but the
//     spirit — refuse low-quality matches — is the same.)
func (p *Parser) Resolve(results []*parse.ParseResult) (*parse.ResolvedGraph, error) {
	out := &parse.ResolvedGraph{}

	// fileBySrcID: map a caller's node ID back to its file path so the
	// locality filter has something to compare against. Built once over
	// the union of nodes from all files.
	fileBySrcID := map[string]string{}
	byName := map[string][]types.Node{}
	for _, r := range results {
		out.Nodes = append(out.Nodes, r.Nodes...)
		out.Edges = append(out.Edges, r.Edges...)
		for _, n := range r.Nodes {
			fileBySrcID[n.ID] = n.FilePath
			if n.Type == types.NodeFunction || n.Type == types.NodeMethod || n.Type == types.NodeClass {
				byName[n.Name] = append(byName[n.Name], n)
			}
		}
	}

	for _, r := range results {
		for _, pr := range r.Pending {
			candidates := byName[pr.TargetQName]
			if len(candidates) == 0 {
				continue
			}
			callerFile := fileBySrcID[pr.SrcID]
			pick, conf := chooseCandidate(candidates, callerFile)
			if pick.ID == "" {
				continue
			}
			out.Edges = append(out.Edges, types.Edge{
				Src: pr.SrcID, Dst: pick.ID, Type: pr.EdgeType,
				Line: pr.Line, Count: 1, Confidence: conf,
			})
		}
	}
	return out, nil
}

// chooseCandidate applies the locality + ambiguity rules to pick a
// single edge target and the confidence label that goes with it.
//
// Algorithm:
//   - Filter to candidates in the same file as the caller. If exactly
//     one survives → INFERRED to that candidate.
//   - If two or more survive (same-file overload), still prefer the
//     same-file slice but flag AMBIGUOUS — TS overload signatures share
//     a name and the CKG schema can't tell them apart without type info.
//   - If zero same-file candidates: fall through to the global pool.
//     One survivor → INFERRED. Two or more → AMBIGUOUS, pick by
//     highest PageRank (or first if rank is unset) so the edge has a
//     concrete destination.
func chooseCandidate(candidates []types.Node, callerFile string) (types.Node, types.Confidence) {
	// Same-file slice.
	var local []types.Node
	for _, c := range candidates {
		if callerFile != "" && c.FilePath == callerFile {
			local = append(local, c)
		}
	}
	if len(local) == 1 {
		return local[0], types.ConfInferred
	}
	if len(local) > 1 {
		return pickByPageRank(local), types.ConfAmbiguous
	}
	// No same-file candidates — global resolution.
	if len(candidates) == 1 {
		return candidates[0], types.ConfInferred
	}
	return pickByPageRank(candidates), types.ConfAmbiguous
}

// pickByPageRank returns the candidate with the highest PageRank. PageRank
// is zero before the buildpipe scoring pass — for a fresh-parse Resolve
// call (e.g. unit tests) the comparison degenerates to "first wins",
// which is still deterministic if the caller passes a stable order.
func pickByPageRank(cands []types.Node) types.Node {
	best := cands[0]
	for _, c := range cands[1:] {
		if c.PageRank > best.PageRank {
			best = c
		}
	}
	return best
}
