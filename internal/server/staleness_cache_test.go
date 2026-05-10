package server

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// TestStalenessCache_HitWithinTTL locks in the debounce contract:
// repeated calls within the TTL window hit the cache and skip the
// git spawn entirely. Drives the counting compute function via
// dependency injection so the test never shells out.
func TestStalenessCache_HitWithinTTL(t *testing.T) {
	var calls int64
	cache := newStalenessCache(5 * time.Second)
	cache.compute = func(m persist.Manifest) (string, bool) {
		atomic.AddInt64(&calls, 1)
		return "abc123", false
	}
	frozen := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return frozen }

	m := persist.Manifest{SrcCommit: "abc", SrcRoot: "/repo"}
	for i := 0; i < 10; i++ {
		cur, stale := cache.get(m)
		if cur != "abc123" || stale {
			t.Errorf("call %d: got (%q, %v), want (abc123, false)", i, cur, stale)
		}
	}
	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Errorf("compute called %d times, want 1 (TTL debounce broken)", got)
	}
}

// TestStalenessCache_RefreshAfterTTL verifies the cache repopulates
// once the TTL window expires. Without this, a stale-banner update
// would never surface to the viewer until ckg serve restart.
func TestStalenessCache_RefreshAfterTTL(t *testing.T) {
	var calls int64
	results := []struct {
		cur   string
		stale bool
	}{
		{"first", false},
		{"second", true},
	}
	cache := newStalenessCache(time.Second)
	cache.compute = func(m persist.Manifest) (string, bool) {
		i := atomic.AddInt64(&calls, 1)
		r := results[(int(i)-1)%len(results)]
		return r.cur, r.stale
	}
	t0 := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	currentTime := t0
	cache.now = func() time.Time { return currentTime }

	m := persist.Manifest{SrcCommit: "abc", SrcRoot: "/repo"}
	cur1, stale1 := cache.get(m)
	if cur1 != "first" || stale1 {
		t.Fatalf("call 1 = (%q, %v), want (first, false)", cur1, stale1)
	}

	// Advance past the TTL — next call must recompute and surface
	// the second result.
	currentTime = t0.Add(2 * time.Second)
	cur2, stale2 := cache.get(m)
	if cur2 != "second" || !stale2 {
		t.Fatalf("call 2 = (%q, %v), want (second, true)", cur2, stale2)
	}
	if got := atomic.LoadInt64(&calls); got != 2 {
		t.Errorf("compute called %d times, want 2 (TTL refresh broken)", got)
	}
}

// TestStalenessCache_KeyInvalidation covers the manifest-changed
// path: a fresh build shifts SrcCommit, which must force a recompute
// even within the TTL window. Otherwise the operator would see the
// previous build's stale flag persist.
func TestStalenessCache_KeyInvalidation(t *testing.T) {
	var calls int64
	cache := newStalenessCache(5 * time.Second)
	cache.compute = func(m persist.Manifest) (string, bool) {
		atomic.AddInt64(&calls, 1)
		return m.SrcCommit + "-current", false
	}
	frozen := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return frozen }

	cur1, _ := cache.get(persist.Manifest{SrcCommit: "abc", SrcRoot: "/repo"})
	cur2, _ := cache.get(persist.Manifest{SrcCommit: "def", SrcRoot: "/repo"})

	if cur1 != "abc-current" || cur2 != "def-current" {
		t.Errorf("key change should refresh: got (%q, %q)", cur1, cur2)
	}
	if got := atomic.LoadInt64(&calls); got != 2 {
		t.Errorf("compute calls = %d, want 2 (one per distinct key)", got)
	}
}
