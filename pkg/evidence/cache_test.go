package evidence

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// countingStore wraps fakeStore with atomic counters so tests can
// assert the cache actually skips heavy work on a hit (AllNodes /
// AllEdges / GetBlob shouldn't fire on the second call when the
// manifest key is unchanged).
type countingStore struct {
	*fakeStore
	allNodesCalls atomic.Int64
	allEdgesCalls atomic.Int64
	getBlobCalls  atomic.Int64
	manifest      persist.Manifest
	mu            sync.Mutex
}

func newCountingStore(nodes []types.Node, edges []types.Edge, blobs map[string][]byte, key string) *countingStore {
	return &countingStore{
		fakeStore: &fakeStore{nodes: nodes, edges: edges, blobs: blobs},
		manifest:  persist.Manifest{BuildTimestamp: key, SrcCommit: key},
	}
}

func (c *countingStore) AllNodes() ([]types.Node, error) {
	c.allNodesCalls.Add(1)
	return c.fakeStore.AllNodes()
}
func (c *countingStore) AllEdges() ([]types.Edge, error) {
	c.allEdgesCalls.Add(1)
	return c.fakeStore.AllEdges()
}
func (c *countingStore) GetBlob(id string) ([]byte, error) {
	c.getBlobCalls.Add(1)
	return c.fakeStore.GetBlob(id)
}
func (c *countingStore) GetManifest() (persist.Manifest, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.manifest, nil
}

// setKey simulates a graph.db rebuild: BuildTimestamp + SrcCommit drift.
func (c *countingStore) setKey(key string) {
	c.mu.Lock()
	c.manifest = persist.Manifest{BuildTimestamp: key, SrcCommit: key}
	c.mu.Unlock()
}

// TestCache_HitSkipsHeavyWork covers the core promise: two BuildPack
// calls with an unchanged manifest should fire the heavy AllNodes /
// AllEdges / GetBlob exactly once between them.
func TestCache_HitSkipsHeavyWork(t *testing.T) {
	store := newCountingStore(
		[]types.Node{
			{ID: "c1", Type: types.NodeCommit, QualifiedName: "commit:aaaa",
				Signature: "1700000100: fix panel jitter", Confidence: types.ConfExtracted},
			{ID: "h1", Type: types.NodeHunk,
				QualifiedName: "hunk:aaaa:Panel.tsx:0",
				FilePath:      "Panel.tsx", StartLine: 1, EndLine: 5,
				Confidence: types.ConfExtracted},
		},
		nil,
		map[string][]byte{"h1": gz("@@ panel jitter patch body")},
		"key-v1",
	)
	cache := NewCache()

	// First call — cold cache, every heavy method fires.
	_, err := cache.BuildPack(store, Options{Intent: "panel"})
	if err != nil {
		t.Fatalf("first BuildPack: %v", err)
	}
	firstNodes := store.allNodesCalls.Load()
	firstEdges := store.allEdgesCalls.Load()
	firstBlobs := store.getBlobCalls.Load()

	// Second call — manifest key unchanged, cache must hit.
	_, err = cache.BuildPack(store, Options{Intent: "panel"})
	if err != nil {
		t.Fatalf("second BuildPack: %v", err)
	}
	if got := store.allNodesCalls.Load(); got != firstNodes {
		t.Errorf("cache miss on AllNodes: first=%d second=%d (want equal)",
			firstNodes, got)
	}
	if got := store.allEdgesCalls.Load(); got != firstEdges {
		t.Errorf("cache miss on AllEdges: first=%d second=%d (want equal)",
			firstEdges, got)
	}
	// GetBlob fires once during indexing AND once per top-K hunk in
	// groupByCommit (to materialise patch text). The second call still
	// hits groupByCommit so getBlobCalls grows by exactly one per
	// returned hunk — but the corpus-build pass is skipped.
	postCallDelta := store.getBlobCalls.Load() - firstBlobs
	if postCallDelta > int64(len(store.fakeStore.nodes)) {
		t.Errorf("cache miss on GetBlob: delta=%d, want ≤ groupByCommit reach",
			postCallDelta)
	}
}

// TestCache_KeyDriftRebuilds covers manifest invalidation: when
// GetManifest reports a different BuildTimestamp / SrcCommit, the
// cache rebuilds on the next call.
func TestCache_KeyDriftRebuilds(t *testing.T) {
	store := newCountingStore(
		[]types.Node{
			{ID: "c1", Type: types.NodeCommit, QualifiedName: "commit:bbbb",
				Signature: "1700000100: hello", Confidence: types.ConfExtracted},
			{ID: "h1", Type: types.NodeHunk,
				QualifiedName: "hunk:bbbb:x.go:0",
				FilePath:      "x.go", Confidence: types.ConfExtracted},
		},
		nil,
		map[string][]byte{"h1": gz("hello world")},
		"key-v1",
	)
	cache := NewCache()
	if _, err := cache.BuildPack(store, Options{Intent: "hello"}); err != nil {
		t.Fatalf("first BuildPack: %v", err)
	}
	beforeNodes := store.allNodesCalls.Load()

	// Simulate rebuild: drift the key.
	store.setKey("key-v2")
	if _, err := cache.BuildPack(store, Options{Intent: "hello"}); err != nil {
		t.Fatalf("post-drift BuildPack: %v", err)
	}
	if got := store.allNodesCalls.Load(); got <= beforeNodes {
		t.Errorf("AllNodes wasn't called again after key drift: before=%d after=%d",
			beforeNodes, got)
	}
}

// TestCache_InvalidateForcesRebuild — explicit Invalidate clears
// state. Documented public surface for tests / admin tooling that
// wants to reset without a manifest drift.
func TestCache_InvalidateForcesRebuild(t *testing.T) {
	store := newCountingStore(
		[]types.Node{
			{ID: "c1", Type: types.NodeCommit, QualifiedName: "commit:cccc",
				Signature: "1700000200: bar", Confidence: types.ConfExtracted},
			{ID: "h1", Type: types.NodeHunk,
				QualifiedName: "hunk:cccc:y.go:0",
				FilePath:      "y.go", Confidence: types.ConfExtracted},
		},
		nil,
		map[string][]byte{"h1": gz("bar body")},
		"key-v1",
	)
	cache := NewCache()
	if _, err := cache.BuildPack(store, Options{Intent: "bar"}); err != nil {
		t.Fatalf("first: %v", err)
	}
	preInvalidate := store.allNodesCalls.Load()
	cache.Invalidate()
	if cache.CachedKey() != "" {
		t.Errorf("CachedKey after Invalidate = %q, want empty", cache.CachedKey())
	}
	if _, err := cache.BuildPack(store, Options{Intent: "bar"}); err != nil {
		t.Fatalf("post-invalidate: %v", err)
	}
	if got := store.allNodesCalls.Load(); got <= preInvalidate {
		t.Errorf("Invalidate didn't trigger rebuild: pre=%d post=%d",
			preInvalidate, got)
	}
}

// TestCache_ConcurrentBuildsSerialise: many goroutines hitting a cold
// cache should exactly ONCE pay the rebuild cost. Validates the
// double-check-locked rebuild path.
func TestCache_ConcurrentBuildsSerialise(t *testing.T) {
	store := newCountingStore(
		[]types.Node{
			{ID: "c1", Type: types.NodeCommit, QualifiedName: "commit:dddd",
				Signature: "1700000300: race", Confidence: types.ConfExtracted},
			{ID: "h1", Type: types.NodeHunk,
				QualifiedName: "hunk:dddd:z.go:0",
				FilePath:      "z.go", Confidence: types.ConfExtracted},
		},
		nil,
		map[string][]byte{"h1": gz("race body")},
		"key-v1",
	)
	cache := NewCache()

	const N = 16
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			_, _ = cache.BuildPack(store, Options{Intent: "race"})
		}()
	}
	wg.Wait()
	if got := store.allNodesCalls.Load(); got != 1 {
		t.Errorf("AllNodes fired %d times across %d concurrent calls; want 1 (one rebuild)",
			got, N)
	}
}
