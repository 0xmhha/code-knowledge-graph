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

// countingManifestStore is the smallest StoreReader stub that
// answers GetManifest. Every other method panics — the cache wrapper
// must not call them.
type countingManifestStore struct {
	persist.StoreReader
	manifest persist.Manifest
	err      error
	calls    int64
}

func (c *countingManifestStore) GetManifest() (persist.Manifest, error) {
	atomic.AddInt64(&c.calls, 1)
	return c.manifest, c.err
}
