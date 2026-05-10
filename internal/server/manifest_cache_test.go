package server

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// TestCachedManifestStore_OneReadAfterPriming locks in the perf
// optimisation contract: GetManifest hits the underlying store
// exactly once at construction; every subsequent call serves from
// memory. A regression here would drag /api/manifest back to its
// 235ms p50 baseline.
func TestCachedManifestStore_OneReadAfterPriming(t *testing.T) {
	src := &countingManifestStore{
		manifest: persist.Manifest{BuildTimestamp: "2026-05-10", SrcCommit: "abc"},
	}
	cached := newCachedManifestStore(src, nil)
	if got := atomic.LoadInt64(&src.calls); got != 1 {
		t.Fatalf("priming should issue exactly 1 read, got %d", got)
	}
	for i := 0; i < 5; i++ {
		m, err := cached.GetManifest()
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if m.SrcCommit != "abc" {
			t.Errorf("call %d: SrcCommit = %q, want abc", i, m.SrcCommit)
		}
	}
	if got := atomic.LoadInt64(&src.calls); got != 1 {
		t.Errorf("after 5 GetManifest calls, store calls = %d, want 1", got)
	}
}

// TestCachedManifestStore_FallsBackOnPrimeError covers the
// degraded-mode contract: when the priming read errors, every
// subsequent call hits the wrapped store. The test verifies both
// that errors propagate and that we don't silently serve a
// zero-valued manifest, which would mislead the viewer's "is the
// graph stale" check.
func TestCachedManifestStore_FallsBackOnPrimeError(t *testing.T) {
	want := errors.New("simulated open failure")
	src := &countingManifestStore{err: want}
	cached := newCachedManifestStore(src, nil)
	for i := 0; i < 3; i++ {
		_, err := cached.GetManifest()
		if !errors.Is(err, want) {
			t.Errorf("call %d: err = %v, want %v", i, err, want)
		}
	}
	if got := atomic.LoadInt64(&src.calls); got != 4 {
		// 1 prime + 3 GetManifest passthroughs.
		t.Errorf("store calls = %d, want 4 (1 prime + 3 fallthrough)", got)
	}
}

// TestCachedManifestStore_EdgeCountsLazyAndCached locks in the
// EdgeCountsByType lazy-cache contract: zero underlying calls until
// the first request, then exactly one even across concurrent callers.
// Without this an /api/edges/counts spike in viewer boot would re-run
// the 1.98M-row GROUP BY scan once per request.
func TestCachedManifestStore_EdgeCountsLazyAndCached(t *testing.T) {
	src := &countingManifestStore{
		manifest:   persist.Manifest{SrcCommit: "abc"},
		edgeCounts: map[string]int{"calls": 100, "modifies": 50},
	}
	cached := newCachedManifestStore(src, nil)

	// Pre-condition: priming reads manifest once but NOT edges.
	if got := atomic.LoadInt64(&src.edgeCallCount); got != 0 {
		t.Fatalf("priming should not touch EdgeCountsByType, got %d calls", got)
	}

	for i := 0; i < 5; i++ {
		counts, err := cached.EdgeCountsByType()
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if counts["calls"] != 100 || counts["modifies"] != 50 {
			t.Errorf("call %d: %v", i, counts)
		}
	}
	if got := atomic.LoadInt64(&src.edgeCallCount); got != 1 {
		t.Errorf("EdgeCountsByType called %d times, want 1", got)
	}
}

// TestCachedManifestStore_EdgeCountsDefensiveCopy verifies the
// caller-mutation guard: a returned map can be written to without
// poisoning the cache. Future viewer code that does ad-hoc
// post-processing on the counts map shouldn't accidentally corrupt
// other callers' view.
func TestCachedManifestStore_EdgeCountsDefensiveCopy(t *testing.T) {
	src := &countingManifestStore{
		manifest:   persist.Manifest{SrcCommit: "abc"},
		edgeCounts: map[string]int{"calls": 100},
	}
	cached := newCachedManifestStore(src, nil)
	first, _ := cached.EdgeCountsByType()
	first["calls"] = 99999
	first["new_key"] = 1
	second, _ := cached.EdgeCountsByType()
	if second["calls"] != 100 {
		t.Errorf("caller mutation leaked into cache: got %d", second["calls"])
	}
	if _, ok := second["new_key"]; ok {
		t.Errorf("caller-added key leaked into cache")
	}
}

// TestCachedManifestStore_EdgeCountsErrorNotCached ensures a
// transient store error doesn't permanently poison the cache.
// Subsequent calls must retry — otherwise a single SQLite hiccup
// during boot would 500 every /api/edges/counts request for the
// rest of the serve lifetime.
func TestCachedManifestStore_EdgeCountsErrorNotCached(t *testing.T) {
	want := errors.New("transient sqlite hiccup")
	src := &countingManifestStore{
		manifest: persist.Manifest{SrcCommit: "abc"},
		edgeErr:  want,
	}
	cached := newCachedManifestStore(src, nil)
	for i := 0; i < 3; i++ {
		_, err := cached.EdgeCountsByType()
		if !errors.Is(err, want) {
			t.Errorf("call %d: err = %v, want %v", i, err, want)
		}
	}
	if got := atomic.LoadInt64(&src.edgeCallCount); got != 3 {
		t.Errorf("error cache leak: edge calls = %d, want 3 (one per request)", got)
	}

	// Recovery path: clearing the error should let the cache prime
	// on the next call and stay stable from there.
	src.edgeErr = nil
	src.edgeCounts = map[string]int{"calls": 7}
	for i := 0; i < 3; i++ {
		counts, err := cached.EdgeCountsByType()
		if err != nil || counts["calls"] != 7 {
			t.Errorf("recovery call %d: counts=%v err=%v", i, counts, err)
		}
	}
	if got := atomic.LoadInt64(&src.edgeCallCount); got != 4 {
		t.Errorf("after recovery: edge calls = %d, want 4 (3 errors + 1 success cached)", got)
	}
}

// countingManifestStore is the smallest StoreReader stub that
// answers GetManifest + EdgeCountsByType. Every other method panics —
// the cache wrapper must not call them.
type countingManifestStore struct {
	persist.StoreReader
	manifest      persist.Manifest
	err           error
	calls         int64
	edgeCounts    map[string]int
	edgeErr       error
	edgeCallCount int64
}

func (c *countingManifestStore) GetManifest() (persist.Manifest, error) {
	atomic.AddInt64(&c.calls, 1)
	return c.manifest, c.err
}

func (c *countingManifestStore) EdgeCountsByType() (map[string]int, error) {
	atomic.AddInt64(&c.edgeCallCount, 1)
	return c.edgeCounts, c.edgeErr
}
