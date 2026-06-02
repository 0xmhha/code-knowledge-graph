package store_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/store"
)

// TestGoStablenetQuerySmoke verifies the graph-generation pipeline has no
// functional gap, by KEYWORD-QUERYING a real go-stablenet graph and checking
// the results against known ground truth: symbols the parser must have
// discovered + indexed, and the FTS keyword index. If a parser/indexer
// regression drops these, the query returns the wrong thing and this fails.
//
// Opt-in: set CKG_GSN_GRAPH to a graph.db file (or a dir containing one), built
// via `ckg build --src <go-stablenet> --out <dir>`. Skipped otherwise.
func TestGoStablenetQuerySmoke(t *testing.T) {
	dbPath := os.Getenv("CKG_GSN_GRAPH")
	if dbPath == "" {
		t.Skip("set CKG_GSN_GRAPH to a graph.db file (or a dir containing one) to run the go-stablenet query smoke")
	}
	if info, err := os.Stat(dbPath); err == nil && info.IsDir() {
		dbPath = filepath.Join(dbPath, "graph.db")
	}
	r, err := store.OpenReadOnly(dbPath)
	if err != nil {
		t.Fatalf("open graph %q: %v", dbPath, err)
	}
	defer func() { _ = r.Close() }()

	// (1) FindSymbol resolves known symbols to the expected file => the parser
	// discovered + indexed them with correct location (graph-gen integrity).
	// exact=false => suffix match on qualified_name (the short-name lookup the
	// find_symbol MCP tool exposes). exact=true matches the full qualified_name.
	findExpect := func(name, wantQname, wantFile string) {
		t.Helper()
		ns, ferr := r.FindSymbol(name, false, store.FindSymbolOptions{})
		if ferr != nil {
			t.Fatalf("FindSymbol(%q): %v", name, ferr)
		}
		for _, n := range ns {
			if n.QualifiedName == wantQname {
				if n.FilePath != wantFile {
					t.Errorf("FindSymbol(%q): %s file=%q, want %q", name, wantQname, n.FilePath, wantFile)
				}
				return
			}
		}
		t.Errorf("FindSymbol(%q): expected %q not found among %d results", name, wantQname, len(ns))
	}
	findExpect("InsertChain", "core.BlockChain.InsertChain", "core/blockchain.go")
	findExpect("NewBlockChain", "core.NewBlockChain", "core/blockchain.go")

	// (2) StableNet-specific (non-Ethereum) code was parsed: WBFT QuorumSize.
	if ns, ferr := r.FindSymbol("QuorumSize", false, store.FindSymbolOptions{}); ferr != nil {
		t.Fatalf("FindSymbol(QuorumSize): %v", ferr)
	} else {
		var inWBFT bool
		for _, n := range ns {
			if strings.HasPrefix(n.FilePath, "consensus/wbft/") {
				inWBFT = true
			}
		}
		if !inWBFT {
			t.Errorf("expected a QuorumSize node under consensus/wbft/, got %d results", len(ns))
		}
	}

	// (3) Keyword FTS search returns scored, relevant hits => the FTS index was
	// built over the generated graph.
	hits, ferr := r.SearchFTS("quorum", 20, store.SearchFTSOptions{})
	if ferr != nil {
		t.Fatalf("SearchFTS(quorum): %v", ferr)
	}
	if len(hits) == 0 {
		t.Fatal("SearchFTS(quorum) returned 0 hits")
	}
	// G1 Score contract (runtime, real data): every hit's Score is in [0,1] and
	// hits are returned in non-increasing Score order — the contract cks's
	// rerankers depend on.
	var relevant bool
	prev := 2.0
	for i, h := range hits {
		if h.Score < 0 || h.Score > 1 {
			t.Errorf("hit %d (%s) Score=%f out of [0,1] (G1 contract)", i, h.Node.QualifiedName, h.Score)
		}
		if h.Score > prev+1e-9 {
			t.Errorf("hits not in non-increasing Score order at %d: %f > prev %f", i, h.Score, prev)
		}
		prev = h.Score
		if strings.Contains(strings.ToLower(h.Node.QualifiedName), "quorum") {
			relevant = true
		}
	}
	if !relevant {
		t.Errorf("expected at least one 'quorum' keyword hit whose name contains quorum, got %d hits", len(hits))
	}
	t.Logf("SearchFTS(quorum): %d hits, top score=%.3f, relevant=%v (G1: Score in [0,1] + descending OK)",
		len(hits), hits[0].Score, relevant)
}
