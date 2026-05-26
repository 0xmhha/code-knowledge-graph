// Package bm25 provides Okapi BM25 ranking for code-aware corpora.
//
// # Stability
//
// This package is part of the public import surface of
// github.com/0xmhha/code-knowledge-graph. The exported API —
// [Scorer], [Okapi], [NewOkapi], [Document], [ScoredDoc], [Tokenize],
// and the two default constants — follows semantic versioning:
//
//   - Minor releases may add new types, methods, or fields.
//   - Patch releases fix bugs without changing the API surface.
//   - The Scorer interface will not gain methods in a minor release;
//     a breaking change to Scorer requires a new major version.
//
// External consumers (ckv, cks) should depend on the [Scorer]
// interface and construct via [NewOkapi]. Direct field access on
// [Okapi] (K1, B) is stable for tuning but the unexported fields
// are not part of the contract.
//
// # Typical usage (external consumer)
//
//	import "github.com/0xmhha/code-knowledge-graph/pkg/bm25"
//
//	scorer := bm25.NewOkapi()
//	scorer.Index([]bm25.Document{
//	    {ID: "node-1", Tokens: bm25.Tokenize("HandleDeposit")},
//	    {ID: "node-2", Tokens: bm25.Tokenize("processWithdraw")},
//	})
//	hits := scorer.TopK(bm25.Tokenize("deposit"), 10)
package bm25
