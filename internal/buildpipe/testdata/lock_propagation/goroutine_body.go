package lockprop

import "sync"

// GoroutineBody exercises W-A §5.0 Q4 — but with a *negative* assertion
// after W-A review (2026-05-11 Important #2): named-function goroutine
// bodies are NOT reached by the propagator in V0.
//
// Apply() locks mu and calls helperWithLock() inline. helperWithLock
// spawns `go gh.touchAsync()`. The propagator follows `calls` + `invokes`
// edges (Q3), but the Go parser does not emit a `calls` edge for the
// `go gh.touchAsync()` shape — it emits a `spawns` edge instead
// (concurrency.go:530-531 "Named-function goroutine body" known gap).
// Consequently `touchAsync.gh.value` does NOT receive an
// accessed_under_lock edge under the current propagator.
//
// Q4 ("Goroutine body INFERRED") therefore lands as a *forward-compatible
// confidence policy* — when a future PR adds `spawns` to the propagator's
// adjacency (or fixes the parser's named-fn goroutine emit), the existing
// uniform-INFERRED path will already produce the right confidence label
// without further code change. Until then this fixture asserts the gap is
// known and reproducible, not that Q4 fires.
//
// Implication for lock_propagation_test.go: GoroutineHolder.touchAsync
// must NOT have an accessed_under_lock edge after flag-ON build.
// Adding `spawns` to buildCalleeAdjacency is tracked as a W-A follow-up.
type GoroutineHolder struct {
	mu    sync.Mutex
	value int
}

func (gh *GoroutineHolder) Apply(delta int) {
	gh.mu.Lock()
	defer gh.mu.Unlock()
	gh.helperWithLock(delta)
}

func (gh *GoroutineHolder) helperWithLock(delta int) {
	go gh.touchAsync(delta)
}

func (gh *GoroutineHolder) touchAsync(delta int) {
	gh.value += delta
}
