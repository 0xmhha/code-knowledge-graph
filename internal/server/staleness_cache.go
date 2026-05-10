package server

import (
	"sync"
	"time"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// stalenessCache debounces computeStaleness calls so /api/manifest's
// `git rev-parse HEAD` (or path-aware `git log -1 -- relPath`) spawn
// only fires once per `ttl`-window per (SrcCommit, SrcRoot) key.
// Captures the residual ~64ms p50 cost the manifest endpoint paid
// after the manifest cache landed (commit 473f839); the underlying
// kv read is now O(1) from memory, so the spawn dominates.
//
// Trade-off: a fresh `ckg build` that drops a new graph.db while
// serve is running won't surface a "stale" indicator until the cache
// TTL expires (or the operator restarts ckg serve). 5s is short
// enough that no human hits the stale window in practice — viewer
// polls the manifest endpoint sparsely (boot + manual refresh).
type stalenessCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	expires time.Time
	current string
	stale   bool
	key     string
	// compute lets tests substitute a counting / deterministic
	// computeStaleness without spawning git. Default is the live
	// implementation in staleness.go.
	compute func(persist.Manifest) (string, bool)
	// now lets tests freeze the clock when verifying TTL behaviour.
	now func() time.Time
}

// stalenessCacheTTL is the production debounce window. Tuned to be
// short enough that operators don't see a stale indicator linger
// after a real rebuild, long enough that the viewer's poll cadence
// (boot + manual refresh) doesn't burn through git spawns.
const stalenessCacheTTL = 5 * time.Second

func newStalenessCache(ttl time.Duration) *stalenessCache {
	return &stalenessCache{
		ttl:     ttl,
		compute: computeStaleness,
		now:     time.Now,
	}
}

// get returns the (current, stale) pair for m, debounced. Cache is
// keyed on (SrcCommit, SrcRoot) so a graph rebuild + serve restart
// surfaces fresh state on the first /api/manifest call after restart.
func (c *stalenessCache) get(m persist.Manifest) (string, bool) {
	key := m.SrcCommit + "|" + m.SrcRoot
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.key == key && c.now().Before(c.expires) {
		return c.current, c.stale
	}
	cur, stale := c.compute(m)
	c.current = cur
	c.stale = stale
	c.key = key
	c.expires = c.now().Add(c.ttl)
	return cur, stale
}
