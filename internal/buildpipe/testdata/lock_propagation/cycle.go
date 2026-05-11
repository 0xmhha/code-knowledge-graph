package lockprop

import "sync"

// Cycle exercises W-A's visited-set defence against call-graph cycles.
// Apply() locks mu, then calls cycleA. cycleA calls cycleB, which calls
// back into cycleA — a 2-node cycle. Without the visited set the DFS
// would never terminate. With the visited set, each node is visited at
// most once per root.
//
// Expected: at least one accessed_under_lock(Cycle.value -> Cycle.mu)
// emitted (cycleA touches the field at the end of its body), and the
// build completes without hanging.
type Cycle struct {
	mu    sync.Mutex
	value int
	flag  bool
}

func (c *Cycle) Apply() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cycleA()
}

func (c *Cycle) cycleA() {
	if c.flag {
		c.cycleB()
	}
	c.value++
}

func (c *Cycle) cycleB() {
	c.cycleA()
}
