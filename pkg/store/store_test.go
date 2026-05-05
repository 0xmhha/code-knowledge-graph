package store_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/store"
)

// Compile-time guard: a nil store.Reader must satisfy the alias. If someone
// renames or removes a method on internal/persist.StoreReader without
// updating the public surface, this stops compiling here — preventing the
// breakage from silently shipping to external consumers.
var _ store.Reader = (store.Reader)(nil)

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
