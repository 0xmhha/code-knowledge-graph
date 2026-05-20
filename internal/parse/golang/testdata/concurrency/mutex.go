// Package mutex_fixture exercises the B1 Stage 1 concurrency pass:
// sync.Mutex / sync.RWMutex fields and locals, basic Lock/Unlock pairs,
// embedded mutex, defer pattern. See concurrency_test.go for assertions.
package mutex_fixture

import "sync"

// Counter has a sync.Mutex field. Expected: NodeMutex named "mu" with
// sub_kind="mutex".
type Counter struct {
	mu    sync.Mutex
	count int
}

// Inc demonstrates the defer-Unlock pattern. Expected lock edges:
//
//	acquires_lock(Counter.Inc -> Counter.mu)
//	releases_lock(Counter.Inc -> Counter.mu)   // from defer
func (c *Counter) Inc() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count++
}

// Get demonstrates the explicit Lock+Unlock pattern (no defer).
func (c *Counter) Get() int {
	c.mu.Lock()
	v := c.count
	c.mu.Unlock()
	return v
}

// Cache uses sync.RWMutex. Expected: NodeMutex sub_kind="rwmutex".
// RLock/RUnlock should also produce acquires_lock/releases_lock (the
// RW variant is encoded in sub_kind on the Mutex node, not the edge).
type Cache struct {
	mu   sync.RWMutex
	data map[string]string
}

func (c *Cache) Read(k string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.data[k]
}

func (c *Cache) Write(k, v string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[k] = v
}

// Embedded mutex pattern (R2.3 in spec). `s.Lock()` is dispatched to the
// embedded sync.Mutex's Lock method — types.Info should resolve correctly.
type Embedded struct {
	sync.Mutex
	val int
}

func (e *Embedded) Set(v int) {
	e.Lock()
	defer e.Unlock()
	e.val = v
}

// LocalLock exercises function-local var sync.Mutex declaration.
func LocalLock() {
	var localMu sync.Mutex
	localMu.Lock()
	defer localMu.Unlock()
}

// FakeMutex is a user-defined type with a Lock() method. The B1 false-positive
// guard (R2.1 in spec) must NOT emit acquires_lock for f.Lock() — types.Info
// confirms FakeMutex is not sync.Mutex.
type FakeMutex struct{}

func (f *FakeMutex) Lock()   {}
func (f *FakeMutex) Unlock() {}

func UseFake() {
	var f FakeMutex
	f.Lock()
	_ = 0 // intentionally empty critical section — fixture tests false-positive guard
	f.Unlock()
}
