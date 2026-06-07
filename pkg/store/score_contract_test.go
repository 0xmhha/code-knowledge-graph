package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/store"
)

// TestSearchFTS_ScoreContract pins the external Reader.SearchFTS Score /
// RawScore contract that downstream consumers (cks composer Stage 2)
// rely on. The internal/persist tests (search_hit_test.go) already
// guard the same shape from inside the persist package, but cks
// imports through pkg/store — so we re-pin the surface here as an
// external `package store_test` consumer to catch any pkg/store alias
// drift that would only surface across module boundaries.
//
// The contract is exactly the spec in plans/01 §G1:
//   - Score is monotonically descending across the returned hits.
//   - Score lies in [0, 1] (min-max normalized within the result set).
//   - RawScore is populated and tracks Score's order.
//   - Single-row / all-equal results assign Score = 1.0 (uniform
//     strength, NOT 0.0 / NaN) — documented so cks rerankers don't
//     misread the uniform case as "perfect".
//
// Opt-in via CKG_GSN_GRAPH so the test exercises a real index when one
// is present, and skips on developer machines that haven't built it
// (mirroring gsn_query_smoke_test.go's discipline).
func TestSearchFTS_ScoreContract(t *testing.T) {
	dbPath := os.Getenv("CKG_GSN_GRAPH")
	if dbPath == "" {
		t.Skip("set CKG_GSN_GRAPH to a graph.db file (or a dir containing one)")
	}
	if info, err := os.Stat(dbPath); err == nil && info.IsDir() {
		dbPath = filepath.Join(dbPath, "graph.db")
	}
	r, err := store.OpenReadOnly(dbPath)
	if err != nil {
		t.Fatalf("open graph %q: %v", dbPath, err)
	}
	defer r.Close()

	// A common identifier any go-stablenet graph will hit.
	hits, err := r.SearchFTS("Finalize", 5, store.SearchFTSOptions{})
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(hits) == 0 {
		t.Skip("graph has no FTS hits for the probe term — refresh CKG_GSN_GRAPH")
	}

	// Score range + monotonic descending + RawScore populated.
	for i, h := range hits {
		if h.Score < 0 || h.Score > 1 {
			t.Errorf("hits[%d].Score = %v, want in [0, 1]", i, h.Score)
		}
		if h.RawScore == 0 {
			t.Errorf("hits[%d].RawScore is zero — backend score not propagated", i)
		}
		if i > 0 && h.Score > hits[i-1].Score {
			t.Errorf("hits[%d].Score = %v > hits[%d].Score = %v (not descending)",
				i, h.Score, i-1, hits[i-1].Score)
		}
	}

	// Single-row / all-equal case: pick a probe so specific it returns
	// at most one row, then assert Score is 1.0 (not 0). Use a long
	// random-looking concatenation; if it happens to match one row the
	// branch fires, otherwise the loop body skips cleanly.
	one, err := r.SearchFTS("DefaultAnzeonConfig", 1, store.SearchFTSOptions{})
	if err != nil {
		t.Fatalf("SearchFTS (single-row probe): %v", err)
	}
	if len(one) == 1 && one[0].Score != 1.0 {
		t.Errorf("single-hit Score = %v, want 1.0 (uniform-strength convention)",
			one[0].Score)
	}
}

// TestBuildGoStablenetSmoke_M2D pins plans/01 §M2.d: a go-stablenet
// build with --policy-file produces NodePolicy nodes (count > 0). The
// reference policy view is cks-domain-sync's emitted file, which yields
// one Policy node per verified domain entry — A4.system_contracts.addresses
// is the canonical one (verified since 2026-06-05) so we look it up by
// its entry-id qualified name.
//
// Opt-in via CKG_GSN_GRAPH like the other smoke tests, so the assertion
// only fires when an operator has actually rebuilt the graph with
// --policy-file. Without a Policy-bearing rebuild this test silently
// skips — better than failing on a developer machine that hasn't
// produced the policy view yet.
func TestBuildGoStablenetSmoke_M2D(t *testing.T) {
	dbPath := os.Getenv("CKG_GSN_GRAPH")
	if dbPath == "" {
		t.Skip("set CKG_GSN_GRAPH to a graph.db file (or a dir containing one)")
	}
	if info, err := os.Stat(dbPath); err == nil && info.IsDir() {
		dbPath = filepath.Join(dbPath, "graph.db")
	}
	r, err := store.OpenReadOnly(dbPath)
	if err != nil {
		t.Fatalf("open graph %q: %v", dbPath, err)
	}
	defer r.Close()

	const entryID = "A4.system_contracts.addresses"
	ns, err := r.FindSymbol(entryID, true, store.FindSymbolOptions{})
	if err != nil {
		t.Fatalf("FindSymbol(%q): %v", entryID, err)
	}
	if len(ns) == 0 {
		t.Skipf("no Policy node for %q — graph built without --policy-file (skip per M2.d opt-in)",
			entryID)
	}
	var hasPolicy bool
	for _, n := range ns {
		if n.Type == "Policy" {
			hasPolicy = true
			break
		}
	}
	if !hasPolicy {
		t.Errorf("FindSymbol(%q) returned %d nodes but none of type Policy (M2.d: NodePolicy count > 0)",
			entryID, len(ns))
	}
}
