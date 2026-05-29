package lockprop

import "sync"

// GoroutineBody exercises W-A §5.0 Q4 (Goroutine body INFERRED). The
// 2026-05-11 W-A landed missed this case because the Go parser did not
// emit a `calls` edge for the `go gh.touchAsync()` shape — only a
// `spawns` edge from parent to the Goroutine node, leaving the named-
// function callee unreachable to the propagator's calls/invokes DFS.
//
// P2 #8 fix (statements.go GoStmt case): the parser now also queues a
// PendingRef for the call inside `go x.method()`, so Pass 2 Resolve
// materialises a calls (or invokes, for interface dispatch) edge from
// the parent function to the goroutine target. The propagator then
// reaches `touchAsync.gh.value` through that edge and emits an
// accessed_under_lock(touchAsync.gh.value, GoroutineHolder.mu) row
// at INFERRED confidence — Q4's "goroutine path is the lowest-trust"
// policy lands automatically via the uniform cross-function INFERRED
// label, no extra confidence-handling needed.
//
// Anonymous goroutine literals — `go func(){…}()` — still go through
// the intra-fn parent-attribution path, not this one, because there's
// no resolvable target qname for a literal body.
//
// Implication for lock_propagation_test.go: GoroutineHolder.touchAsync
// MUST have an accessed_under_lock edge to GoroutineHolder.mu after
// flag-ON build (post-P2 #8). The TestLockPropagation_NamedGoroutine
// case pins this.
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
