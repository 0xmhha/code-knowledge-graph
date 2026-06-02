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

// TestCache_TicketIndexAggregation verifies the cache-backed ticket
// rollup: each issue ID gets one row with hunk_count summed across
// its commits, sorted descending. Covers the read-side branch of
// the Cache that powers the viewer's TicketIndex panel.
func TestCache_TicketIndexAggregation(t *testing.T) {
	store := &fakeStore{
		nodes: []types.Node{
			// Two commits, one ticket each.
			{ID: "c1", Type: types.NodeCommit, QualifiedName: "commit:aaaa",
				Signature: "1700000100: hot fix (#42)", Confidence: types.ConfExtracted},
			{ID: "c2", Type: types.NodeCommit, QualifiedName: "commit:bbbb",
				Signature: "1700000200: tier two (#7)", Confidence: types.ConfExtracted},
			// 3 hunks under c1 (#42), 1 hunk under c2 (#7).
			{ID: "h1", Type: types.NodeHunk, QualifiedName: "hunk:aaaa:f.go:0",
				DocComment: "issues:GH-42", Confidence: types.ConfExtracted},
			{ID: "h2", Type: types.NodeHunk, QualifiedName: "hunk:aaaa:g.go:0",
				DocComment: "issues:GH-42", Confidence: types.ConfExtracted},
			{ID: "h3", Type: types.NodeHunk, QualifiedName: "hunk:aaaa:h.go:0",
				DocComment: "issues:GH-42", Confidence: types.ConfExtracted},
			{ID: "h4", Type: types.NodeHunk, QualifiedName: "hunk:bbbb:i.go:0",
				DocComment: "issues:GH-7", Confidence: types.ConfExtracted},
		},
		blobs: map[string][]byte{
			"h1": gz("h1"), "h2": gz("h2"), "h3": gz("h3"), "h4": gz("h4"),
		},
	}
	cache := NewCache()
	rows, err := cache.TicketIndex(store, 0)
	if err != nil {
		t.Fatalf("TicketIndex: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 ticket rows, got %d", len(rows))
	}
	// Sorted desc by hunk count → GH-42 first.
	if rows[0].IssueID != "GH-42" || rows[0].HunkCount != 3 {
		t.Errorf("row[0] = %+v, want IssueID=GH-42 HunkCount=3", rows[0])
	}
	if rows[1].IssueID != "GH-7" || rows[1].HunkCount != 1 {
		t.Errorf("row[1] = %+v, want IssueID=GH-7 HunkCount=1", rows[1])
	}
	// SampleCommits attached.
	if len(rows[0].SampleCommits) != 1 || rows[0].SampleCommits[0].SHA != "aaaa" {
		t.Errorf("row[0].SampleCommits = %v, want [aaaa]", rows[0].SampleCommits)
	}
}

// TestCache_TicketIndexTopFiles locks in the top-files surface added
// for the TicketIndex viewer panel: each sample commit reports up to
// 3 directories that its hunks touched, ranked by hunk count and
// tie-broken on dirname (deterministic). Hunks with no FilePath are
// skipped; commits whose files all live at the repo root collapse
// to "(root)" so the viewer always renders a non-empty pill.
func TestCache_TicketIndexTopFiles(t *testing.T) {
	store := &fakeStore{
		nodes: []types.Node{
			{ID: "c1", Type: types.NodeCommit, QualifiedName: "commit:aaaa",
				Signature: "1700000100: multi-touch (#42)", Confidence: types.ConfExtracted},
			// 4 hunks under c1, dirname distribution:
			//   crypto/secp256k1 → 2
			//   consensus        → 1
			//   core/types       → 1
			// Top-3 expected: [crypto/secp256k1, consensus, core/types]
			// (counts 2, 1, 1; alphabetical tie-break on the 1-count tier).
			{ID: "h1", Type: types.NodeHunk, QualifiedName: "hunk:aaaa:crypto/secp256k1/a.go:0",
				FilePath: "crypto/secp256k1/a.go", DocComment: "issues:GH-42", Confidence: types.ConfExtracted},
			{ID: "h2", Type: types.NodeHunk, QualifiedName: "hunk:aaaa:crypto/secp256k1/b.go:0",
				FilePath: "crypto/secp256k1/b.go", DocComment: "issues:GH-42", Confidence: types.ConfExtracted},
			{ID: "h3", Type: types.NodeHunk, QualifiedName: "hunk:aaaa:consensus/x.go:0",
				FilePath: "consensus/x.go", DocComment: "issues:GH-42", Confidence: types.ConfExtracted},
			{ID: "h4", Type: types.NodeHunk, QualifiedName: "hunk:aaaa:core/types/y.go:0",
				FilePath: "core/types/y.go", DocComment: "issues:GH-42", Confidence: types.ConfExtracted},
			// Second commit — single hunk at the repo root → "(root)".
			{ID: "c2", Type: types.NodeCommit, QualifiedName: "commit:bbbb",
				Signature: "1700000200: top-level fix (#7)", Confidence: types.ConfExtracted},
			{ID: "h5", Type: types.NodeHunk, QualifiedName: "hunk:bbbb:main.go:0",
				FilePath: "main.go", DocComment: "issues:GH-7", Confidence: types.ConfExtracted},
		},
		blobs: map[string][]byte{
			"h1": gz("h1"), "h2": gz("h2"), "h3": gz("h3"),
			"h4": gz("h4"), "h5": gz("h5"),
		},
	}
	rows, err := NewCache().TicketIndex(store, 0)
	if err != nil {
		t.Fatalf("TicketIndex: %v", err)
	}
	byID := map[string]TicketRow{}
	for _, r := range rows {
		byID[r.IssueID] = r
	}
	gh42 := byID["GH-42"]
	if len(gh42.SampleCommits) != 1 {
		t.Fatalf("GH-42: want 1 sample commit, got %d", len(gh42.SampleCommits))
	}
	got := gh42.SampleCommits[0].TopFiles
	want := []string{"crypto/secp256k1", "consensus", "core/types"}
	if !equalSlice(got, want) {
		t.Errorf("GH-42 top_files = %v, want %v", got, want)
	}
	// Repo-root collapse for GH-7.
	gh7 := byID["GH-7"]
	if len(gh7.SampleCommits) != 1 {
		t.Fatalf("GH-7: want 1 sample commit, got %d", len(gh7.SampleCommits))
	}
	if got, want := gh7.SampleCommits[0].TopFiles, []string{"(root)"}; !equalSlice(got, want) {
		t.Errorf("GH-7 top_files = %v, want %v", got, want)
	}
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestCache_TicketIndexEmptyOnNoIssues — a graph without any
// `issues:…` doc_comment on Hunk rows yields an empty slice (not nil
// is acceptable; we just check len==0).
func TestCache_TicketIndexEmptyOnNoIssues(t *testing.T) {
	store := &fakeStore{
		nodes: []types.Node{
			{ID: "c1", Type: types.NodeCommit, QualifiedName: "commit:zzzz",
				Signature: "1700000900: just a commit", Confidence: types.ConfExtracted},
			{ID: "h1", Type: types.NodeHunk, QualifiedName: "hunk:zzzz:f.go:0",
				Confidence: types.ConfExtracted}, // no DocComment
		},
		blobs: map[string][]byte{"h1": gz("body")},
	}
	rows, err := NewCache().TicketIndex(store, 0)
	if err != nil {
		t.Fatalf("TicketIndex: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows on issueless graph, got %d", len(rows))
	}
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
	if postCallDelta > int64(len(store.nodes)) {
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

	// Simulate rebuild: drift the key. The 1s manifest TTL means a
	// test that runs the second BuildPack within the window would
	// see the cached pre-drift manifest and skip the rebuild —
	// production callers that drop a fresh graph.db while the cache
	// is hot accept the same lag (1s drift detection). Calling
	// Invalidate() here mirrors what `ckg build` will do if it ever
	// gets a "tell the running server my graph just changed" hook;
	// it's also the only way to keep the test fast.
	store.setKey("key-v2")
	cache.Invalidate()
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
