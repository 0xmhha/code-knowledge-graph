package parse

import (
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// PendingRef is an unresolved cross-file reference produced in Pass 1
// and resolved (or marked AMBIGUOUS) in Pass 2.
//
// DispatchKind (Track C P1b, schema 1.7): optional metadata for `invokes`
// edges. Empty for static `calls`. Carried through Pass 2 Resolve so the
// final edge row preserves the dispatch classification decided at AST time
// (when types.Info is still in scope).
type PendingRef struct {
	SrcID       string
	EdgeType    types.EdgeType
	TargetQName string
	HintFile    string
	Line        int
	// ByteOffset (W-C W6 V2.15, 2026-05-15): byte position of the
	// referent in the source file. Used by the Solidity resolver to
	// disambiguate same-line shadow scopes — when V2.0's line-only
	// `declLine <= useSiteLine <= scopeEndLine` filter admits multiple
	// decls, the byte offset breaks the tie via containment. Zero =
	// unset (other parsers leave it default; Sol resolver falls back
	// to V2.0 line-only behavior when bytes are missing).
	ByteOffset int
	// Order (W-C W7.3, 2026-05-18): source-order index. Carried to the
	// resolved Edge.Order field. Currently populated by Sol's
	// runHasModifier walker; ignored elsewhere.
	Order        int
	DispatchKind string
}

// ParseResult is the per-file output of Pass 1.
type ParseResult struct {
	Path    string
	Nodes   []types.Node
	Edges   []types.Edge
	Pending []PendingRef
}

// ResolvedGraph is the per-language Pass 2 output: in addition to the union of
// per-file results, edges that resolved or were marked AMBIGUOUS.
type ResolvedGraph struct {
	Nodes []types.Node
	Edges []types.Edge
}

// Parser is the contract every language parser implements.
type Parser interface {
	// ParseFile runs Pass 1 on a single file. Pure function — must be safe
	// to call concurrently from a worker pool.
	ParseFile(path string, src []byte) (*ParseResult, error)

	// Resolve runs Pass 2 over the union of ParseResults from the same language.
	Resolve(results []*ParseResult) (*ResolvedGraph, error)

	// Extensions reports the file extensions this parser handles (lowercase, with leading ".").
	Extensions() []string
}
