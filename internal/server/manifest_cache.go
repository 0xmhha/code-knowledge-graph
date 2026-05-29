package server

import (
	"log/slog"
	"maps"
	"sync"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// cachedManifestStore wraps a persist.StoreReader so build-time-fixed
// reads return from memory after the first call. Originally written
// for GetManifest (commit 473f839); now also caches EdgeCountsByType,
// whose `SELECT type, COUNT(*) FROM edges GROUP BY type` p99 jitter
// dominates the /api/edges/counts SLO at scale despite the
// `idx_edges_type` covering index. Embeds the interface so every
// other method passes through unmodified — matches the
// llmSafeStoreReader pattern used in internal/mcp.
//
// Why this lives at the server layer (not persist): the lifetime that
// matters is one `ckg serve` invocation. graph.db rebuilds today
// require an explicit serve restart; codifying that here avoids
// pulling stale-detection plumbing into the storage interface.
type cachedManifestStore struct {
	persist.StoreReader

	// manifest is primed eagerly at construction (single small kv
	// read; no reason to defer the cost).
	manifest persist.Manifest
	cached   bool

	// EdgeCountsByType is primed lazily on the first call — `SELECT
	// type, COUNT(*) FROM edges GROUP BY type` walks 1.98M rows on a
	// large graph (~150ms steady-state, p99 several hundred ms). The
	// mutex protects the {edgeCounts, edgeCountsCached} pair from
	// concurrent first-callers. Errors are NOT cached: a transient
	// SQLite hiccup shouldn't permanently poison the cache.
	edgeCountsMu     sync.Mutex
	edgeCounts       map[string]int
	edgeCountsCached bool
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

// EdgeCountsByType serves /api/edges/counts from a process-lifetime
// cache after the first hit. Returns a defensive copy so callers
// (which today happen to be JSON encoders, but might tomorrow be
// anything that mutates) can't poison the shared map.
func (c *cachedManifestStore) EdgeCountsByType() (map[string]int, error) {
	c.edgeCountsMu.Lock()
	defer c.edgeCountsMu.Unlock()
	if !c.edgeCountsCached {
		counts, err := c.StoreReader.EdgeCountsByType()
		if err != nil {
			return nil, err
		}
		c.edgeCounts = counts
		c.edgeCountsCached = true
	}
	return cloneCountsMap(c.edgeCounts), nil
}

// cloneCountsMap returns a shallow copy. Edge type counts are O(30)
// at most — a fresh map per call is negligible vs the ~150ms read it
// avoids.
func cloneCountsMap(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	maps.Copy(out, m)
	return out
}
