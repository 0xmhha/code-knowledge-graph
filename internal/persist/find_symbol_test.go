package persist_test

import (
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// newKindsFixtureStore inserts three nodes that share a common suffix
// ("MarkerHit") but belong to different NodeTypes, so opts.Kinds can be
// exercised in isolation. Mirrors the cks `arch_explain` shape where
// the same name appears as Function / Type / Interface across a code
// area and the consumer wants to ask for "all kinds I care about" in
// one round-trip rather than fetching N times.
func newKindsFixtureStore(t *testing.T) persist.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "kinds.db")
	s, err := persist.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	nodes := []types.Node{
		{
			ID: "kfn0000000000001", Type: types.NodeFunction,
			Name: "MarkerHit", QualifiedName: "p.MarkerHit",
			FilePath: "p/fn.go", Language: "go",
			Confidence: types.ConfExtracted,
		},
		{
			ID: "kif0000000000001", Type: types.NodeInterface,
			Name: "MarkerHit", QualifiedName: "iface.MarkerHit",
			FilePath: "iface/i.go", Language: "go",
			Confidence: types.ConfExtracted,
		},
		{
			ID: "kst0000000000001", Type: types.NodeStruct,
			Name: "MarkerHit", QualifiedName: "st.MarkerHit",
			FilePath: "st/s.go", Language: "go",
			Confidence: types.ConfExtracted,
		},
	}
	if err := s.InsertNodes(nodes); err != nil {
		t.Fatalf("InsertNodes: %v", err)
	}
	return s
}

// TestFindSymbol_KindsSingle locks the CKG-4 contract for the
// single-kind case: opts.Kinds restricts the result set in SQL,
// so the consumer no longer needs to over-fetch + post-filter.
func TestFindSymbol_KindsSingle(t *testing.T) {
	s := newKindsFixtureStore(t)

	nodes, err := s.FindSymbol("MarkerHit", false, persist.FindSymbolOptions{
		Kinds: []types.NodeType{types.NodeFunction},
	})
	if err != nil {
		t.Fatalf("FindSymbol: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("got %d nodes, want 1", len(nodes))
	}
	if nodes[0].Type != types.NodeFunction {
		t.Errorf("got node type %q, want Function", nodes[0].Type)
	}
	if nodes[0].ID != "kfn0000000000001" {
		t.Errorf("got node ID %q, want kfn0000000000001", nodes[0].ID)
	}
}

// TestFindSymbol_KindsMultiple verifies that the SQL `type IN (...)`
// emits the right set when Kinds carries more than one entry — the
// exact shape the cks `arch_explain` intent needs (fns + types +
// interfaces in one query).
func TestFindSymbol_KindsMultiple(t *testing.T) {
	s := newKindsFixtureStore(t)

	nodes, err := s.FindSymbol("MarkerHit", false, persist.FindSymbolOptions{
		Kinds: []types.NodeType{types.NodeFunction, types.NodeInterface},
	})
	if err != nil {
		t.Fatalf("FindSymbol: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("got %d nodes, want 2", len(nodes))
	}
	got := map[types.NodeType]bool{}
	for _, n := range nodes {
		got[n.Type] = true
	}
	if !got[types.NodeFunction] || !got[types.NodeInterface] {
		t.Errorf("missing expected kinds; got %v", got)
	}
	if got[types.NodeStruct] {
		t.Errorf("Struct leaked despite Kinds={Function, Interface}")
	}
}

// TestFindSymbol_KindsEmptyMatchesAll asserts the zero-value Kinds
// disables the filter — backward-compatible behavior so callers that
// don't care about kinds keep their semantics.
func TestFindSymbol_KindsEmptyMatchesAll(t *testing.T) {
	s := newKindsFixtureStore(t)

	nodes, err := s.FindSymbol("MarkerHit", false, persist.FindSymbolOptions{})
	if err != nil {
		t.Fatalf("FindSymbol: %v", err)
	}
	if len(nodes) != 3 {
		t.Errorf("got %d nodes without Kinds filter, want 3", len(nodes))
	}
}

// TestFindSymbol_KindsNoMatch ensures a kind with zero rows returns
// empty (not the unfiltered set or an error).
func TestFindSymbol_KindsNoMatch(t *testing.T) {
	s := newKindsFixtureStore(t)

	nodes, err := s.FindSymbol("MarkerHit", false, persist.FindSymbolOptions{
		Kinds: []types.NodeType{types.NodeMethod},
	})
	if err != nil {
		t.Fatalf("FindSymbol: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("got %d nodes for Kinds=Method, want 0", len(nodes))
	}
}
