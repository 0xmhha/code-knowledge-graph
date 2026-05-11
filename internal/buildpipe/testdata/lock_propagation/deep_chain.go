package lockprop

import "sync"

// DeepChain exercises W-A §5.0 Q1 (DFS depth=5). Caller locks mu and
// calls a 3-hop helper chain (level0 -> level1 -> level2 -> level3 ->
// terminal). terminal touches the protected field. Without DFS the
// propagator wouldn't reach the terminal; depth>=4 is required.
//
// Expected: accessed_under_lock(DeepChain.value -> DeepChain.mu) — emitted
// once via the propagator. The intra-function pass sees no field access
// inside DeepChain.Enter (it only calls level0), so the edge is purely
// from W-A.
type DeepChain struct {
	mu    sync.Mutex
	value int
}

func (d *DeepChain) Enter() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.level0()
}

func (d *DeepChain) level0() { d.level1() }
func (d *DeepChain) level1() { d.level2() }
func (d *DeepChain) level2() { d.level3() }
func (d *DeepChain) level3() { d.terminal() }
func (d *DeepChain) terminal() {
	d.value = 42
}
