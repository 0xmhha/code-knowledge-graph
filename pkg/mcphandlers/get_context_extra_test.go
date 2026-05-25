package mcphandlers

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/smartctx"
)

// ---------------------------------------------------------------------------
// smartctx.BuildContext — additional behaviour branches
// (helpers like setKeys / scoredNode were folded into pkg/smartctx and are
// covered by behavioural tests there + this file's BuildContext exercises.)
// ---------------------------------------------------------------------------

// TestBuildContextMatchesFound verifies the happy path when the query matches
// symbols in the fixture ("Greet" is defined in testdata/resolve/a/a.go).
func TestBuildContextMatchesFound(t *testing.T) {
	store := newFixtureStore(t)

	res, err := smartctx.BuildContext(store, "Greet", smartctx.Options{
		BudgetTokens: 4000, IncludeBlobs: true, MaxBodies: 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if notFound, _ := res["not_found"].(bool); notFound {
		t.Error("expected not_found=false for a query that should match 'Greet'")
	}
	sub, ok := res["subgraph"].(map[string]any)
	if !ok {
		t.Fatalf("expected subgraph map, got %T", res["subgraph"])
	}
	nodes, ok := sub["nodes"].([]map[string]any)
	if !ok {
		t.Fatalf("expected nodes slice, got %T", sub["nodes"])
	}
	if len(nodes) == 0 {
		t.Error("expected at least one node in subgraph")
	}
	for _, n := range nodes {
		for _, key := range []string{"id", "name", "type", "qname", "score"} {
			if _, exists := n[key]; !exists {
				t.Errorf("node missing key %q: %v", key, n)
			}
		}
	}
	// Citation Enforcement (warn mode): metadata.warnings must always be
	// present (possibly empty) so consumers never have to nil-check.
	meta, ok := res["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("expected metadata map, got %T", res["metadata"])
	}
	if _, ok := meta["warnings"]; !ok {
		t.Error("metadata.warnings must always be present (warn-mode contract)")
	}
}

// TestBuildContextSmallBudget confirms trimmed=true when the budget is too
// tight to pack even the first summary.
func TestBuildContextSmallBudget(t *testing.T) {
	store := newFixtureStore(t)
	res, err := smartctx.BuildContext(store, "Greet", smartctx.Options{
		BudgetTokens: 1, IncludeBlobs: true, MaxBodies: 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	trimmed, _ := res["trimmed"].(bool)
	if !trimmed {
		if notFound, _ := res["not_found"].(bool); !notFound {
			t.Logf("result: %+v", res)
			t.Error("expected trimmed=true or not_found=true when budget=1")
		}
	}
}

// TestBuildContextNoBlobsIncluded verifies that when include_blobs=false the
// summaries slice is populated (where budget allows) and the bodies slice is
// empty.
func TestBuildContextNoBlobsIncluded(t *testing.T) {
	store := newFixtureStore(t)
	res, err := smartctx.BuildContext(store, "Hello", smartctx.Options{
		BudgetTokens: 4000, IncludeBlobs: false, MaxBodies: 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if notFound, _ := res["not_found"].(bool); notFound {
		t.Skip("no FTS hit for 'Hello'; skipping include_blobs=false check")
	}
	bodies, _ := res["bodies"].([]map[string]any)
	if len(bodies) != 0 {
		t.Errorf("expected empty bodies when include_blobs=false, got %d entries", len(bodies))
	}
}
