// Package lockprop is the W-A (D1 Stage B) cross-function lock propagation
// fixture. Each file isolates one scenario from the spec test plan
// (docs/design/go-cross-function-lock-propagation.md §4.2 / §6).
package lockprop

import "sync"

// SingleHop exercises the canonical W-A positive case:
//
//	Caller locks mu, then calls touch(); touch() reads/writes the protected
//	field s.value WITHOUT itself acquiring the lock. The intra-function B1
//	pass emits NO accessed_under_lock edge for s.value because the
//	function that touches it (touch) holds no lock. W-A's DFS walks
//	Caller.Apply --calls--> Caller.touch and emits the missing edge.
//
// Expected after --lock-propagation:
//
//	accessed_under_lock(SingleHop.value -> SingleHop.mu)  // INFERRED (W-A §5.0 Q2)
type SingleHop struct {
	mu    sync.Mutex
	value int
}

// Apply locks, then delegates the field mutation to touch.
func (s *SingleHop) Apply(delta int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.touch(delta)
}

// touch performs the actual mutation. It holds no lock — relies on every
// caller to wrap the call. W-A propagates the edge from Apply's lock state.
func (s *SingleHop) touch(delta int) {
	s.value += delta
}
