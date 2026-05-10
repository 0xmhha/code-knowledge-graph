package server

import (
	"log/slog"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// cachedManifestStore wraps a persist.StoreReader so GetManifest is a
// single in-memory dereference after the first call. Embeds the
// interface so every other method passes through unmodified —
// matches the llmSafeStoreReader pattern used in internal/mcp.
//
// Why this lives at the server layer (not persist): the lifetime that
// matters is one `ckg serve` invocation. graph.db rebuilds today
// require an explicit serve restart; codifying that here avoids
// pulling stale-detection plumbing into the storage interface.
type cachedManifestStore struct {
	persist.StoreReader
	manifest persist.Manifest
	cached   bool
}

// newCachedManifestStore reads the manifest once at construction.
// Failures fall through to the wrapped StoreReader on every call —
// callers see the same error path they had before (the original
// p50=235ms kv read on every /api/manifest).
func newCachedManifestStore(store persist.StoreReader, log *slog.Logger) *cachedManifestStore {
	c := &cachedManifestStore{StoreReader: store}
	m, err := store.GetManifest()
	if err != nil {
		if log != nil {
			log.Warn("server: manifest cache priming failed; falling back to per-call reads", "err", err)
		}
		return c
	}
	c.manifest = m
	c.cached = true
	return c
}

// GetManifest returns the cached value when priming succeeded;
// otherwise delegates to the wrapped store on every call (matches
// pre-cache behaviour).
func (c *cachedManifestStore) GetManifest() (persist.Manifest, error) {
	if c.cached {
		return c.manifest, nil
	}
	return c.StoreReader.GetManifest()
}
