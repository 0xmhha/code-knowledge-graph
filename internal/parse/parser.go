package parse

import (
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// PendingRef is an unresolved cross-file reference produced in Pass 1
// and resolved (or marked AMBIGUOUS) in Pass 2.
type PendingRef struct {
	SrcID       string
	EdgeType    types.EdgeType
	TargetQName string
	HintFile    string
	Line        int
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
