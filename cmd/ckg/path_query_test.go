package main

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

func TestIsTestPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"", false},
		{"core/txpool/legacypool/legacypool.go", false},
		{"eth/gasprice/anzeon.go", false},
		{"core/txpool/legacypool/legacypool_test.go", true},
		{"pkg/foo/testdata/fixture.go", true},
		{"internal/parse/testutil/helper.go", true},
		{"web/src/app/__tests__/app.js", true},
		{"web/src/app/button.test.tsx", true},
		{"web/src/app/button.spec.ts", true},
	}
	for _, c := range cases {
		if got := isTestPath(c.path); got != c.want {
			t.Errorf("isTestPath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestHistoryEdgeForPath(t *testing.T) {
	historical := []types.EdgeType{types.EdgeChangedIn, types.EdgeBlame, types.EdgeHasHunk, types.EdgeAdjacent}
	for _, e := range historical {
		if !historyEdgeForPath(e) {
			t.Errorf("historyEdgeForPath(%q) = false, want true", e)
		}
	}
	structural := []types.EdgeType{types.EdgeCalls, types.EdgeInvokes, types.EdgeDefines, types.EdgeUsesType}
	for _, e := range structural {
		if historyEdgeForPath(e) {
			t.Errorf("historyEdgeForPath(%q) = true, want false (structural edge)", e)
		}
	}
}

func TestResolveNodeQuery(t *testing.T) {
	nodes := []types.Node{
		{ID: "n1", Name: "reset", QualifiedName: "rlp.encBuffer.reset", FilePath: "rlp/encbuffer.go", PageRank: 0.9},
		{ID: "n2", Name: "reset", QualifiedName: "legacypool.LegacyPool.reset", FilePath: "core/txpool/legacypool/legacypool.go", PageRank: 0.1},
		{ID: "n3", Name: "reset", QualifiedName: "foo.reset", FilePath: "foo/foo_test.go", PageRank: 0.99},
	}

	// Exact qualified name resolves deterministically regardless of PageRank.
	if id, _ := resolveNodeQuery(nodes, "legacypool.LegacyPool.reset", true); id != "n2" {
		t.Errorf("exact qname: got %q, want n2", id)
	}
	// Dotted suffix disambiguates a partially qualified name (the G1 win).
	if id, _ := resolveNodeQuery(nodes, "LegacyPool.reset", true); id != "n2" {
		t.Errorf("suffix qname: got %q, want n2", id)
	}
	// Bare name with excludeTests: the high-PageRank test node n3 is excluded,
	// so the non-test highest-PageRank n1 wins; hits counts only non-test (2).
	if id, hits := resolveNodeQuery(nodes, "reset", true); id != "n1" || hits != 2 {
		t.Errorf("bare excludeTests: got (%q,%d), want (n1,2)", id, hits)
	}
	// Bare name including tests: n3 (highest PageRank) wins; hits = 3.
	if id, hits := resolveNodeQuery(nodes, "reset", false); id != "n3" || hits != 3 {
		t.Errorf("bare includeTests: got (%q,%d), want (n3,3)", id, hits)
	}
}
