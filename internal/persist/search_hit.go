package persist

import "github.com/0xmhha/code-knowledge-graph/pkg/types"

// SearchFTSOptions configures filter push-down for StoreReader.SearchFTS.
// Zero value means "no filter" — every match passes through.
//
// Filters that the persistence layer cannot or chooses not to push down
// (e.g. path globs cheap on the client) are deliberately absent. Adding
// them later is a non-breaking change because struct fields default to
// zero on omission.
//
// See docs/followups-from-cks-dogfood-2026-05-19.md item CKG-2 for the
// downstream motivation: cks currently over-fetches by FilterOverfetchRatio=3
// and post-filters client-side on Language, which caps recall when filters
// drop most of a small page.
type SearchFTSOptions struct {
	// Language pushes a WHERE language = ? predicate into the SQL.
	// Empty string disables the predicate (no language filter).
	Language string
}

// SearchHit pairs a node with its full-text search relevance score.
//
// Returned by StoreReader.SearchFTS so downstream rerankers can
// distinguish "one strong unique-identifier hit" from "five weak
// common-word hits" — the gap that drove the cks workaround at
// internal/ckgclient/real.go (1 - i/(N+1) fake score, see
// docs/followups-from-cks-dogfood-2026-05-19.md item CKG-1).
//
// Two scores are exposed:
//
//   - Score: result-set min-max normalized to [0, 1]. Comparable
//     within a single SearchFTS call. NOT comparable across calls —
//     different result sets have different min/max windows.
//     Recommended field for downstream rerankers.
//
//   - RawScore: backend-native score, retained for debugging or
//     advanced rerankers that already know the backend's scale.
//     SQLite: -bm25(nodes_fts), sign-flipped so higher is better.
//     PostgreSQL: ts_rank(search_vector, plainto_tsquery).
//     The two scales differ — do NOT cross-compare RawScore across
//     backends.
type SearchHit struct {
	Node     types.Node
	Score    float64 // normalized to [0, 1], result-set local
	RawScore float64 // backend-native, higher = stronger match
}

// normalizeSearchHits applies result-set min-max normalization to the
// Score field of each hit. RawScore is assumed to be populated already.
//
// Degenerate case (all RawScore values equal — single-row result or
// perfect tie): Score is set to 1.0 for every row. This signals
// "uniform strength" to the consumer rather than collapsing to 0.0
// (which would falsely imply weak matches) or NaN (which would
// silently corrupt downstream rerank arithmetic).
func normalizeSearchHits(hits []SearchHit) {
	if len(hits) == 0 {
		return
	}
	minRaw, maxRaw := hits[0].RawScore, hits[0].RawScore
	for _, h := range hits[1:] {
		if h.RawScore < minRaw {
			minRaw = h.RawScore
		}
		if h.RawScore > maxRaw {
			maxRaw = h.RawScore
		}
	}
	span := maxRaw - minRaw
	if span == 0 {
		for i := range hits {
			hits[i].Score = 1.0
		}
		return
	}
	for i := range hits {
		hits[i].Score = (hits[i].RawScore - minRaw) / span
	}
}
