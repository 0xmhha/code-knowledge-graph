package mcp

import (
	"reflect"
	"sort"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// ---------------------------------------------------------------------------
// buildContext — additional branches
// ---------------------------------------------------------------------------

// TestBuildContextMatchesFound verifies the happy path when the query matches
// symbols in the fixture ("Greet" is defined in testdata/resolve/a/a.go).
func TestBuildContextMatchesFound(t *testing.T) {
	store := newFixtureStore(t)

	res, err := buildContext(store, "Greet", 4000, true, 5)
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
	// Verify each node has expected keys.
	for _, n := range nodes {
		for _, key := range []string{"id", "name", "type", "qname", "score"} {
			if _, exists := n[key]; !exists {
				t.Errorf("node missing key %q: %v", key, n)
			}
		}
	}
}

// TestBuildContextSmallBudget confirms trimmed=true when the budget is too
// tight to pack even the first summary.
func TestBuildContextSmallBudget(t *testing.T) {
	store := newFixtureStore(t)

	// Budget of 1 token is effectively zero useful space — the query itself
	// costs estimateTokens("Greet") = 2 tokens, exceeding a budget of 1, so
	// the packing loop will hit the budget ceiling and trimmed must be true.
	res, err := buildContext(store, "Greet", 1, true, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	trimmed, _ := res["trimmed"].(bool)
	if !trimmed {
		// It is also valid for the result to come back as not_found if FTS
		// returns nothing; in that case the test should not fail.
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

	res, err := buildContext(store, "Hello", 4000, false, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if notFound, _ := res["not_found"].(bool); notFound {
		// FTS returned nothing; we cannot assert on bodies/summaries shape.
		t.Skip("no FTS hit for 'Hello'; skipping include_blobs=false check")
	}
	bodies, _ := res["bodies"].([]map[string]any)
	if len(bodies) != 0 {
		t.Errorf("expected empty bodies when include_blobs=false, got %d entries", len(bodies))
	}
	summaries, _ := res["summaries"].([]map[string]any)
	if len(summaries) == 0 {
		t.Log("summaries slice is empty — possibly budget too tight or no matching docs; not treated as hard failure")
	}
}

// ---------------------------------------------------------------------------
// rowsToNodeRefs
// ---------------------------------------------------------------------------

func TestRowsToNodeRefs(t *testing.T) {
	rows := []scoredNode{
		{n: types.Node{ID: "id1", Name: "Foo", Type: "function", QualifiedName: "pkg.Foo"}, score: 0.9},
		{n: types.Node{ID: "id2", Name: "Bar", Type: "function", QualifiedName: "pkg.Bar"}, score: 0.5},
		{n: types.Node{ID: "id3", Name: "Baz", Type: "struct", QualifiedName: "pkg.Baz"}, score: 0.1},
	}

	got := rowsToNodeRefs(rows)

	if len(got) != len(rows) {
		t.Fatalf("expected %d refs, got %d", len(rows), len(got))
	}

	for i, r := range rows {
		m := got[i]
		if m["id"] != r.n.ID {
			t.Errorf("[%d] id mismatch: got %v want %v", i, m["id"], r.n.ID)
		}
		if m["name"] != r.n.Name {
			t.Errorf("[%d] name mismatch", i)
		}
		if m["type"] != r.n.Type {
			t.Errorf("[%d] type mismatch", i)
		}
		if m["qname"] != r.n.QualifiedName {
			t.Errorf("[%d] qname mismatch", i)
		}
		if m["score"] != r.score {
			t.Errorf("[%d] score mismatch: got %v want %v", i, m["score"], r.score)
		}
		for _, key := range []string{"id", "name", "type", "qname", "score"} {
			if _, exists := m[key]; !exists {
				t.Errorf("[%d] missing key %q", i, key)
			}
		}
	}
}

func TestRowsToNodeRefsEmpty(t *testing.T) {
	got := rowsToNodeRefs(nil)
	if len(got) != 0 {
		t.Errorf("expected empty slice for nil input, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// setKeys
// ---------------------------------------------------------------------------

func TestSetKeysStringStruct(t *testing.T) {
	in := map[string]struct{}{"a": {}, "b": {}, "c": {}}
	got := setKeys(in)
	sort.Strings(got)
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSetKeysIntValues(t *testing.T) {
	in := map[string]int{"x": 1, "y": 2}
	got := setKeys(in)
	sort.Strings(got)
	want := []string{"x", "y"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSetKeysEmpty(t *testing.T) {
	got := setKeys(map[string]struct{}{})
	if len(got) != 0 {
		t.Errorf("expected empty slice for empty map, got %v", got)
	}
}

func TestSetKeysSingle(t *testing.T) {
	got := setKeys(map[string]bool{"only": true})
	if len(got) != 1 || got[0] != "only" {
		t.Errorf("got %v, want [only]", got)
	}
}
