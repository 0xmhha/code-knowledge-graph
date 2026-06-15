package mcphandlers

import (
	"testing"
	"time"

	"github.com/0xmhha/code-knowledge-graph/pkg/store"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestChangeHistory covers the change_history core: bare-name resolution,
// PR aggregation (deduped by number, newest-first, k-capped), and the
// ambiguous / not_found envelopes — exercised without the MCP closure.
func TestChangeHistory(t *testing.T) {
	base := newFixtureStore(t)

	// Resolve a unique symbol to its node ID, then inject PR breadcrumbs
	// against that ID so the PR-attachment branch runs without a git fixture.
	hits, err := base.FindSymbol("coll1.UseHasher", true, store.FindSymbolOptions{})
	if err != nil || len(hits) == 0 {
		t.Fatalf("seed lookup: err=%v hits=%d", err, len(hits))
	}
	id := hits[0].ID
	store := &prInjectingStore{StoreReader: base, prByNode: map[string][]types.PRRef{
		id: {
			{Number: 85, Title: "fix: reject stale-view justifications", MergedAtUTC: mustT("2026-06-01T00:00:00Z")},
			{Number: 84, Title: "fix: prevent forgery", MergedAtUTC: mustT("2026-05-27T00:00:00Z")},
			{Number: 85, Title: "dup ignored", MergedAtUTC: mustT("2026-06-01T00:00:00Z")},
		},
	}}

	t.Run("bare name resolves and returns deduped, newest-first PRs", func(t *testing.T) {
		res, err := changeHistory(store, "UseHasher", time.Time{}, 20)
		if err != nil {
			t.Fatal(err)
		}
		if res["seed_qname"] != "coll1.UseHasher" {
			t.Fatalf("seed_qname=%v", res["seed_qname"])
		}
		prs, _ := res["prs"].([]map[string]any)
		if len(prs) != 2 {
			t.Fatalf("expected 2 deduped PRs, got %d: %v", len(prs), prs)
		}
		if prs[0]["number"] != 85 || prs[1]["number"] != 84 {
			t.Errorf("expected newest-first [85,84], got [%v,%v]", prs[0]["number"], prs[1]["number"])
		}
	})

	t.Run("k caps the count", func(t *testing.T) {
		res, _ := changeHistory(store, "UseHasher", time.Time{}, 1)
		prs, _ := res["prs"].([]map[string]any)
		if len(prs) != 1 || prs[0]["number"] != 85 {
			t.Errorf("k=1 should return only newest PR 85, got %v", prs)
		}
	})

	t.Run("ambiguous bare name returns candidates", func(t *testing.T) {
		res, _ := changeHistory(store, "Size", time.Time{}, 20)
		if a, _ := res["ambiguous"].(bool); !a {
			t.Errorf("expected ambiguous=true for Size, got %v", res)
		}
	})

	t.Run("unknown name is not_found", func(t *testing.T) {
		res, _ := changeHistory(store, "NoSuchSymbol", time.Time{}, 20)
		if nf, _ := res["not_found"].(bool); !nf {
			t.Errorf("expected not_found=true, got %v", res)
		}
	})
}

func mustT(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
