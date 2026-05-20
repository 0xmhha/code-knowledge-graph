package persist_test

import (
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// newScoreFixtureStore builds a tiny graph with two nodes deliberately
// designed to produce different BM25 strengths for the same query:
//
//   - strongNode: query token appears in the short `name` column.
//   - weakNode:   query token appears only inside a long `doc_comment`.
//
// SQLite FTS5's BM25 penalises matches in longer fields (doc-length
// normalization), so the strong node should score higher. This is the
// exact ranking signal that CKG-1 surfaces — downstream rerankers
// (cks) need it to distinguish "1 unique-identifier hit" from
// "N common-word hits". See docs/followups-from-cks-dogfood-2026-05-19.md
// item CKG-1.
func newScoreFixtureStore(t *testing.T) persist.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "score.db")
	s, err := persist.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	nodes := []types.Node{
		{
			ID:            "strongnode000000", // 16 chars
			Type:          types.NodeFunction,
			Name:          "QueryToken",
			QualifiedName: "p.QueryToken",
			FilePath:      "p/strong.go",
			Language:      "go",
			Confidence:    types.ConfExtracted,
		},
		{
			ID:            "weaknode00000000", // 16 chars
			Type:          types.NodeFunction,
			Name:          "Unrelated",
			QualifiedName: "p.Unrelated",
			FilePath:      "p/weak.go",
			Language:      "go",
			Confidence:    types.ConfExtracted,
			// Long doc; QueryToken appears once, diluted by surrounding words.
			DocComment: "this function does many other things and only " +
				"mentions QueryToken in passing among many unrelated words " +
				"that make the document long for BM25 normalization purposes",
		},
	}
	if err := s.InsertNodes(nodes); err != nil {
		t.Fatalf("InsertNodes: %v", err)
	}
	if err := s.RebuildFTS(); err != nil {
		t.Fatalf("RebuildFTS: %v", err)
	}
	return s
}

// TestSearchFTS_ScoreMonotonic locks the core CKG-1 contract: a
// stronger BM25 match yields a higher Score than a weaker one.
// Without this, normalizeSearchHits could be silently inverted or
// the SQL ORDER BY could drop and cks would observe scrambled ranks.
func TestSearchFTS_ScoreMonotonic(t *testing.T) {
	s := newScoreFixtureStore(t)

	hits, err := s.SearchFTS("QueryToken", 10)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}

	// The first hit must be the short-name match (BM25 prefers shorter
	// fields). RawScore must also be strictly greater — a tie would mean
	// the doc-length normalization is not in effect.
	if hits[0].Node.ID != "strongnode000000" {
		t.Errorf("expected strongNode first, got %q", hits[0].Node.ID)
	}
	if !(hits[0].RawScore > hits[1].RawScore) {
		t.Errorf("RawScore not strictly descending: %v then %v",
			hits[0].RawScore, hits[1].RawScore)
	}
	// Score is min-max normalized → max becomes 1.0, min becomes 0.0.
	if hits[0].Score != 1.0 {
		t.Errorf("top hit Score = %v, want 1.0", hits[0].Score)
	}
	if hits[1].Score != 0.0 {
		t.Errorf("bottom hit Score = %v, want 0.0", hits[1].Score)
	}
}

// TestSearchFTS_ScoreRangeNormalized asserts every Score falls in
// [0, 1] regardless of backend scale. A regression that bypassed
// normalizeSearchHits would expose raw BM25 (negative for SQLite) to
// downstream consumers.
func TestSearchFTS_ScoreRangeNormalized(t *testing.T) {
	s := newScoreFixtureStore(t)

	hits, err := s.SearchFTS("QueryToken", 10)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	for i, h := range hits {
		if h.Score < 0.0 || h.Score > 1.0 {
			t.Errorf("hits[%d].Score = %v, want in [0,1]", i, h.Score)
		}
	}
}

// TestSearchFTS_SingleHitScoreOne pins the degenerate-case decision:
// a single match (or all-equal raw scores) sets Score = 1.0 rather
// than collapsing to NaN or 0.0. This matters because cks's reranker
// multiplies Score with other signals — a silent 0.0 would zero out
// otherwise-valid evidence.
func TestSearchFTS_SingleHitScoreOne(t *testing.T) {
	s := newScoreFixtureStore(t)

	// "Unrelated" matches only the weakNode.
	hits, err := s.SearchFTS("Unrelated", 10)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected exactly 1 hit, got %d", len(hits))
	}
	if hits[0].Score != 1.0 {
		t.Errorf("single hit Score = %v, want 1.0", hits[0].Score)
	}
}
