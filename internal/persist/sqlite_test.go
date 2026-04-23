package persist_test

import (
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

func TestStoreRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "graph.db")
	store, err := persist.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	want := types.Node{
		ID: "abc123def456ghij", Type: types.NodeFunction,
		Name: "Foo", QualifiedName: "pkg.Foo",
		FilePath: "pkg/foo.go", StartLine: 10, EndLine: 12,
		StartByte: 100, EndByte: 150,
		Language: "go", Confidence: types.ConfExtracted,
	}
	if err := store.InsertNodes([]types.Node{want}); err != nil {
		t.Fatalf("InsertNodes: %v", err)
	}

	got, err := store.GetNode(want.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.QualifiedName != want.QualifiedName || got.Type != want.Type {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, want)
	}
}
