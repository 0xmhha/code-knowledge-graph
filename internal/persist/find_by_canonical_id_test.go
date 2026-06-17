package persist_test

import (
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestFindByCanonicalID covers the exact, unambiguous lookup added for
// symbol-identity Phase 1 (ADR-0001): a hit returns the one node, a miss and an
// empty input both return found=false with no error.
func TestFindByCanonicalID(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "canonical.db")
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
			ID: "cid0000000000001", Type: types.NodeMethod,
			Name: "Size", QualifiedName: "core.Set.Size",
			CanonicalID: "example.com/core.(*Set).Size",
			FilePath:    "core/set.go", Language: "go",
			Confidence: types.ConfExtracted,
		},
		{
			ID: "cid0000000000002", Type: types.NodeMethod,
			Name: "Size", QualifiedName: "wbft.Other.Size",
			CanonicalID: "example.com/consensus/wbft.(*Other).Size",
			FilePath:    "wbft/other.go", Language: "go",
			Confidence: types.ConfExtracted,
		},
		{
			ID: "cid0000000000003", Type: types.NodeFunction,
			Name: "builtin", QualifiedName: "p.builtin",
			// no canonical id (mirrors builtins / AST-only mode)
			FilePath: "p/p.go", Language: "go", Confidence: types.ConfExtracted,
		},
	}
	if err := s.InsertNodes(nodes); err != nil {
		t.Fatalf("InsertNodes: %v", err)
	}

	// exact hit on a colliding short name resolves to exactly one node.
	n, found, err := s.FindByCanonicalID("example.com/consensus/wbft.(*Other).Size")
	if err != nil {
		t.Fatalf("FindByCanonicalID: %v", err)
	}
	if !found {
		t.Fatalf("expected to find wbft Other.Size")
	}
	if n.ID != "cid0000000000002" || n.QualifiedName != "wbft.Other.Size" {
		t.Errorf("resolved wrong node: id=%q qname=%q", n.ID, n.QualifiedName)
	}

	// miss → not found, no error.
	if _, found, err := s.FindByCanonicalID("example.com/does/not.Exist"); err != nil || found {
		t.Errorf("miss: found=%v err=%v, want found=false err=nil", found, err)
	}

	// empty input → not found, no error (never matches the empty-canonical node).
	if _, found, err := s.FindByCanonicalID(""); err != nil || found {
		t.Errorf("empty input: found=%v err=%v, want found=false err=nil", found, err)
	}
}
