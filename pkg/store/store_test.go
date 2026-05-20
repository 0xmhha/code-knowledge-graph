package store_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/store"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Compile-time guard: a nil store.Reader must satisfy the alias. If someone
// renames or removes a method on internal/persist.StoreReader without
// updating the public surface, this stops compiling here — preventing the
// breakage from silently shipping to external consumers.
var _ store.Reader = (store.Reader)(nil)

// Compile-time guard: every value type that an external caller could
// touch via Reader must be reachable through pkg/store. If any of these
// alias declarations stops compiling, an external consumer has lost the
// ability to use Reader without falling back to internal/persist (which
// they cannot reach by the Go `internal/` rule). See CKG-6.
var (
	_ store.SearchHit         = store.SearchHit{}
	_ store.SearchFTSOptions  = store.SearchFTSOptions{}
	_ store.FindSymbolOptions = store.FindSymbolOptions{}
	_ store.Manifest          = store.Manifest{}
	_                         = store.ErrInvalidMetric
)

// TestPublicSurface_CanConstructOptions exercises the option types
// from outside-the-module's perspective: a consumer constructs the
// values it would pass to Reader.SearchFTS / Reader.FindSymbol using
// only pkg/store and pkg/types imports. If a future internal change
// breaks this construction path, an external sister-repo build also
// breaks — this test catches it inside this module first.
func TestPublicSurface_CanConstructOptions(t *testing.T) {
	t.Helper()

	sfts := store.SearchFTSOptions{Language: "go"}
	if sfts.Language != "go" {
		t.Errorf("SearchFTSOptions zero-value or alias misbehaves")
	}

	fs := store.FindSymbolOptions{
		Language: "go",
		Kinds:    []types.NodeType{types.NodeFunction, types.NodeInterface},
	}
	if fs.Language != "go" || len(fs.Kinds) != 2 {
		t.Errorf("FindSymbolOptions field access failed: Language=%q Kinds=%v",
			fs.Language, fs.Kinds)
	}

	// SearchHit is a result, not a request — construct a fake instance
	// to confirm both Node and Score fields are reachable externally.
	hit := store.SearchHit{
		Node:  types.Node{ID: "fake000000000001", Type: types.NodeFunction},
		Score: 1.0,
	}
	if hit.Node.ID == "" || hit.Score == 0 {
		t.Errorf("SearchHit field access failed")
	}
}

// TestPublicManifest_FieldAccess confirms an external consumer can
// construct and read every public Manifest field without falling back
// to internal/persist. This catches regressions where a field gets
// silently moved or unexported during a refactor.
func TestPublicManifest_FieldAccess(t *testing.T) {
	m := store.Manifest{
		CommitHash:     "abc123",
		SchemaVersion:  "1.9",
		IndexTimestamp: "2026-05-20T12:00:00Z",
	}
	if m.CommitHash == "" || m.SchemaVersion == "" || m.IndexTimestamp == "" {
		t.Errorf("Manifest field access failed: %+v", m)
	}
}

// TestOpenReadOnly_Missing_FailsOnUse asserts that a missing DB eventually
// surfaces an error — either eagerly at OpenReadOnly or lazily on first
// query. The underlying SQLite driver is lazy, so the open call alone may
// succeed; the contract is that reads must not silently return zero values.
func TestOpenReadOnly_Missing_FailsOnUse(t *testing.T) {
	r, err := store.OpenReadOnly("/nonexistent/graph.db")
	if err != nil {
		return // eager-fail driver — also acceptable
	}
	defer func() { _ = r.Close() }()
	if _, err := r.GetManifest(); err == nil {
		t.Fatal("expected error reading manifest from missing DB, got nil")
	}
}
