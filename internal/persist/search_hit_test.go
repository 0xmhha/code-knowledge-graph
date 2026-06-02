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
	t.Cleanup(func() { _ = s.Close() })
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

	hits, err := s.SearchFTS("QueryToken", 10, persist.SearchFTSOptions{})
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

	hits, err := s.SearchFTS("QueryToken", 10, persist.SearchFTSOptions{})
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	for i, h := range hits {
		if h.Score < 0.0 || h.Score > 1.0 {
			t.Errorf("hits[%d].Score = %v, want in [0,1]", i, h.Score)
		}
	}
}

// newLangFilterFixtureStore inserts three nodes that all match a single
// FTS token but belong to different languages, so opts.Language can be
// exercised in isolation from BM25 ranking.
func newLangFilterFixtureStore(t *testing.T) persist.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "langfilter.db")
	s, err := persist.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	nodes := []types.Node{
		{
			ID: "lfgo000000000001", Type: types.NodeFunction,
			Name: "Marker", QualifiedName: "pgo.Marker",
			FilePath: "pgo/a.go", Language: "go",
			Confidence: types.ConfExtracted,
		},
		{
			ID: "lfts000000000001", Type: types.NodeFunction,
			Name: "Marker", QualifiedName: "pts.Marker",
			FilePath: "pts/a.ts", Language: "ts",
			Confidence: types.ConfExtracted,
		},
		{
			ID: "lfsol00000000001", Type: types.NodeFunction,
			Name: "Marker", QualifiedName: "psol.Marker",
			FilePath: "psol/a.sol", Language: "sol",
			Confidence: types.ConfExtracted,
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

// TestSearchFTS_LanguagePushdown locks the CKG-2 contract: when
// opts.Language is set, the SQL predicate drops rows from other
// languages BEFORE the LIMIT applies — so cks no longer needs to
// over-fetch (FilterOverfetchRatio=3) and post-filter client-side.
func TestSearchFTS_LanguagePushdown(t *testing.T) {
	s := newLangFilterFixtureStore(t)

	cases := []struct {
		lang   string
		wantID string
	}{
		{"go", "lfgo000000000001"},
		{"ts", "lfts000000000001"},
		{"sol", "lfsol00000000001"},
	}
	for _, c := range cases {
		hits, err := s.SearchFTS("Marker", 10, persist.SearchFTSOptions{Language: c.lang})
		if err != nil {
			t.Fatalf("SearchFTS(lang=%s): %v", c.lang, err)
		}
		if len(hits) != 1 {
			t.Errorf("lang=%s: got %d hits, want 1", c.lang, len(hits))
			continue
		}
		if hits[0].Node.ID != c.wantID {
			t.Errorf("lang=%s: got node %q, want %q", c.lang, hits[0].Node.ID, c.wantID)
		}
		if hits[0].Node.Language != c.lang {
			t.Errorf("lang=%s: returned node has language %q", c.lang, hits[0].Node.Language)
		}
	}
}

// TestSearchFTS_LanguageEmptyMatchesAll asserts that the zero value of
// SearchFTSOptions disables the filter — backward-compatible behavior
// for the Search() adapter and any caller that doesn't care about
// language.
func TestSearchFTS_LanguageEmptyMatchesAll(t *testing.T) {
	s := newLangFilterFixtureStore(t)

	hits, err := s.SearchFTS("Marker", 10, persist.SearchFTSOptions{})
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(hits) != 3 {
		t.Errorf("got %d hits without language filter, want 3", len(hits))
	}
}

// TestSearchFTS_LanguageNoMatch ensures a language with zero matching
// rows returns empty (not the unfiltered result set or an error).
func TestSearchFTS_LanguageNoMatch(t *testing.T) {
	s := newLangFilterFixtureStore(t)

	hits, err := s.SearchFTS("Marker", 10, persist.SearchFTSOptions{Language: "rust"})
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("got %d hits for language=rust, want 0", len(hits))
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
	hits, err := s.SearchFTS("Unrelated", 10, persist.SearchFTSOptions{})
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
