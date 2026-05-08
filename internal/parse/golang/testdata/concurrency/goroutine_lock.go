package mutex_fixture

import "sync"

// goroutine_lock.go exercises Track C P1a: Lock/Unlock CallExprs inside a
// `go func() { ... }()` literal must produce acquires_lock / releases_lock
// edges. Pre-fix, the body walker's GoStmt branch returned false to avoid
// double-walking, but emitGoroutineChannelEdges only handled SendStmt /
// UnaryExpr — so lock calls inside goroutines were silently dropped.
//
// Canonical pattern (mirrors internal/buildpipe/language_runners.go's
// parseConcurrent error-counter accumulation):
//
//	var errMu sync.Mutex
//	go func() {
//	    errMu.Lock()
//	    defer errMu.Unlock()
//	    ...
//	}()
//
// Expected edges (typed mode, EXTRACTED confidence):
//   acquires_lock(GoroutineLock -> goroutine_lock.GoroutineLock.errMu)
//   releases_lock(GoroutineLock -> goroutine_lock.GoroutineLock.errMu)
//
// The Mutex node itself is emitted by scanFuncBodyForMutexLocals
// (independent of the goroutine walk); only the edges depended on the fix.

// GoroutineLock spawns a worker goroutine that locks a function-local
// sync.Mutex. The Lock/Unlock pair must surface as graph edges.
func GoroutineLock() {
	var errMu sync.Mutex
	done := make(chan struct{})
	go func() {
		errMu.Lock()
		defer errMu.Unlock()
		// critical section omitted on purpose — fixture targets edge emission
		close(done)
	}()
	<-done
}

// GoroutineRWLock variant exercises RLock / RUnlock inside the goroutine.
// Both should also map to acquires_lock / releases_lock (the read/write
// distinction lives on the Mutex node's sub_kind, not the edge type).
func GoroutineRWLock() {
	var rwMu sync.RWMutex
	done := make(chan struct{})
	go func() {
		rwMu.RLock()
		defer rwMu.RUnlock()
		close(done)
	}()
	<-done
}
