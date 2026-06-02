package store_test

// Compile-time guard for the R1' C2 surface that cks (doc 03) imports
// in-process. If any symbol below is removed or its name/signature changes,
// this test stops compiling — so the cross-repo contract cannot silently
// drift between ckg and its consumers. It executes nothing.

import (
	"github.com/0xmhha/code-knowledge-graph/pkg/bm25"
	"github.com/0xmhha/code-knowledge-graph/pkg/concurrency"
	"github.com/0xmhha/code-knowledge-graph/pkg/evidence"
	"github.com/0xmhha/code-knowledge-graph/pkg/impact"
	"github.com/0xmhha/code-knowledge-graph/pkg/store"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Package-level functions cks calls.
var (
	_ = store.OpenReadOnly
	_ = store.GetManifest
	_ = concurrency.Analyze // NEW (G7/S1): the concurrency_impact backend
	_ = impact.Compute      // ImpactOfChange
	_ = evidence.BuildPack  // EvidenceForIntent
	_ = bm25.NewOkapi
	_ = bm25.Tokenize
)

// Types cks names.
var (
	_ store.Reader
	_ store.SearchHit
	_ store.Manifest
	_ store.FindSymbolOptions
	_ store.SearchFTSOptions
	_ types.PRRef
)

// Reader methods cks depends on (interface method expressions; compile-only).
var (
	_ = store.Reader.FindSymbol
	_ = store.Reader.NeighborhoodByQname
	_ = store.Reader.SubgraphByQname // GetSubgraph
	_ = store.Reader.SearchFTS       // real Score/RawScore (G1)
	_ = store.Reader.GetNodePRs
	_ = store.Reader.GetManifest
	_ = store.Reader.Close
)
