package lockprop

import "sync"

// GoroutineBody exercises W-A §5.0 Q4: a goroutine entry transitions the
// lock state into the helper, but the helper still gets INFERRED (the
// propagator's uniform confidence — schedule asynchrony means the caller's
// lock may have been released by the time the goroutine runs).
//
// Apply() locks mu and calls helperWithLock() inline. helperWithLock then
// spawns `go gh.touchAsync()`. The intra-function pass sees touchAsync
// invoked via `go` — but the propagator follows the calls edge from
// helperWithLock to touchAsync and emits accessed_under_lock(field, mu).
// Confidence stays INFERRED, matching Q4's "lowest-trust path" decision.
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
