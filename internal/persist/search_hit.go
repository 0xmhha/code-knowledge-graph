package persist

import (
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

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

	// Mode selects how multi-token queries combine. The zero value
	// (empty string) preserves the historical OR-broadening behaviour:
	// rewriteFTSQuery joins tokens with FTS5 OR so any one match
	// surfaces a candidate, then BM25 + PageRank + usage rerank.
	//
	// Mode = "and" engages a post-FTS filter that drops hits whose
	// FTS-indexed columns (name + qualified_name + signature +
	// doc_comment) miss any query token. Mirrors the
	// pkg/evidence/BuildPack Mode="and" semantics so external
	// consumers see consistent AND behaviour across the search and
	// evidence surfaces. Implementation over-fetches (limit × 3,
	// floor 30) before filtering to preserve recall.
	//
	// Mode = "or" is accepted as a synonym of the zero value for
	// callers that want to be explicit. Any other value is treated
	// as "or" (forward-compatible — future modes are append-only).
	Mode string

	// NodeKinds restricts the result set to specific node types. The
	// zero value (nil slice) applies the *default symbol-only filter*:
	// search_text returns only the types that types.NodeType.IsSymbol
	// reports true for, which strips statement-level nodes
	// (IfStmt/LoopStmt/CallSite/ReturnStmt/SwitchStmt/AwaitPoint),
	// meta nodes (Commit/Hunk), and path-only nodes (Import/Export)
	// from FTS hits that match purely on the enclosing symbol's qname
	// prefix.
	//
	// To surface every node type the FTS index matched, pass an
	// explicit slice — typically types.AllNodeTypes() — or list the
	// specific kinds you need. An empty (non-nil) slice is treated
	// the same as nil and applies the default symbol filter; callers
	// that mean "match nothing" should not call SearchFTS at all.
	NodeKinds []types.NodeType
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
