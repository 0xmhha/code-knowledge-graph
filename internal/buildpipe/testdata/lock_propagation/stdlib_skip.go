package lockprop

import (
	"fmt"
	"sync"
)

// StdlibSkip exercises the noise-control guard (W-A §3.3): callees outside
// the build (fmt.Println here) MUST NOT receive accessed_under_lock edges.
// They have no node in g.Nodes, so propagateLockedFieldAccess skips them
// at the adjacency-build step. Verified by asserting no edge in the
// resulting graph targets an external symbol.
type StdlibSkip struct {
	mu sync.Mutex
}

// Log holds mu while calling into stdlib. The fmt.Println call should not
// generate any propagation edge — propagation only emits when the callee
// is a Function/Method node already in the graph.
func (s *StdlibSkip) Log(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Println(msg)
}
