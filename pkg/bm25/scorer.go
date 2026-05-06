// Package bm25 implements Okapi BM25 ranking for code-aware corpora.
//
// The algorithm is the standard Okapi BM25 formulation (Robertson, Walker
// 1994) with the conventional smoothing IDF (`log(1 + (N-n+0.5)/(n+0.5))`).
// The implementation is hand-written in Go from the algorithm description
// alone — no source copied from any library. Behaviour was cross-checked
// against two reference implementations:
//
//   - github.com/blevesearch/bleve (Apache-2.0) — search/scorer/scorer_term.go
//   - github.com/dorianbrown/rank_bm25 (Apache-2.0, Python) — bm25.BM25Okapi
//
// Algorithm authors and reference implementations are credited above; this
// file contains no derivative code from either project.
package bm25

// Document is one indexable record. Tokens are pre-tokenized — call
// Tokenize for the package's standard code-aware splitter, or supply
// custom tokens for a domain-specific corpus.
type Document struct {
	ID     string
	Tokens []string
}

// ScoredDoc pairs a document ID with its BM25 score for the most recent
// query. Score is always >= 0; documents with no matching terms are not
// returned by TopK.
type ScoredDoc struct {
	ID    string
	Score float64
}

// Scorer is the contract every BM25 implementation in this package
// satisfies. The two-phase pattern (Index then Score / TopK) lets callers
// build the corpus once per build and reuse for many queries.
type Scorer interface {
	// Index registers the full corpus. Repeated calls overwrite earlier
	// state — Scorer is not append-only.
	Index(docs []Document)
	// Score returns the BM25 score for one document under one query.
	// Returns 0 when docID is unknown or no query term matches.
	Score(query []string, docID string) float64
	// TopK returns the top-k documents by score, descending. k <= 0
	// returns every matching document.
	TopK(query []string, k int) []ScoredDoc
}
