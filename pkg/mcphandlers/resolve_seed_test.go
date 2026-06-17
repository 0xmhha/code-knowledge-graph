package mcphandlers

import (
	"reflect"
	"testing"
)

// TestResolveSeed exercises the seed-resolution helper that lets the
// graph-traversal tools (find_callers / find_callees / get_subgraph)
// accept a bare short name the same way find_symbol(exact=false) does.
//
// Fixture graph (internal/parse/golang/testdata/resolve):
//
//	a.Greet                — bare "Greet" must resolve here (single match)
//	coll1.Set.Size         — bare "Size" is ambiguous: matches both
//	coll2.Other.Size           Size methods across two packages
func TestResolveSeed(t *testing.T) {
	store := newFixtureStore(t)

	t.Run("exact qname passes through", func(t *testing.T) {
		q, cands, ambiguous, ok := resolveSeed(store, "a.Greet", "")
		if !ok || ambiguous || q != "a.Greet" || cands != nil {
			t.Fatalf("exact: got q=%q cands=%v ambiguous=%v ok=%v", q, cands, ambiguous, ok)
		}
	})

	t.Run("bare name resolves via suffix", func(t *testing.T) {
		q, cands, ambiguous, ok := resolveSeed(store, "Greet", "")
		if !ok || ambiguous || q != "a.Greet" || cands != nil {
			t.Fatalf("suffix: got q=%q cands=%v ambiguous=%v ok=%v", q, cands, ambiguous, ok)
		}
	})

	t.Run("ambiguous bare name lists candidates", func(t *testing.T) {
		q, cands, ambiguous, ok := resolveSeed(store, "Size", "")
		if ok || !ambiguous || q != "" {
			t.Fatalf("ambiguous: got q=%q cands=%v ambiguous=%v ok=%v", q, cands, ambiguous, ok)
		}
		want := []string{"coll1.Set.Size", "coll2.Other.Size"}
		if !reflect.DeepEqual(cands, want) {
			t.Fatalf("ambiguous candidates: got %v want %v", cands, want)
		}
	})

	t.Run("canonical_id resolves precisely past a qname collision", func(t *testing.T) {
		// the bare "Size" is ambiguous (above), but the globally-unique
		// canonical id pins exactly coll1.Set.Size — the core of ADR-0001.
		q, cands, ambiguous, ok := resolveSeed(store, "ckgresolve.test/coll1.(*Set).Size", "")
		if !ok || ambiguous || cands != nil || q != "coll1.Set.Size" {
			t.Fatalf("canonical: got q=%q cands=%v ambiguous=%v ok=%v", q, cands, ambiguous, ok)
		}
	})

	t.Run("unknown name is not_found", func(t *testing.T) {
		q, cands, ambiguous, ok := resolveSeed(store, "NoSuchSymbol", "")
		if ok || ambiguous || q != "" || cands != nil {
			t.Fatalf("not_found: got q=%q cands=%v ambiguous=%v ok=%v", q, cands, ambiguous, ok)
		}
	})

	t.Run("empty seed is not_found", func(t *testing.T) {
		_, _, _, ok := resolveSeed(store, "", "")
		if ok {
			t.Fatal("empty seed should not resolve")
		}
	})
}
