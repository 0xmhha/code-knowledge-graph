package lockprop

// NoLockNoEdge exercises the negative guard: when the caller doesn't lock,
// the callee's field accesses MUST NOT produce accessed_under_lock edges
// even when the callee touches a field in the same struct.
//
// This is the symmetric case to single_hop.go — same structural shape,
// but no Lock() call in the caller. Verifies the propagator's entry
// condition (holdersByFunc empty for HelperWithoutLock) is enforced.
type NoLockNoEdge struct {
	value int
}

// HelperWithoutLock has no Lock call. Its calls to touchUnlocked must
// emit zero accessed_under_lock edges.
func (s *NoLockNoEdge) HelperWithoutLock(delta int) {
	s.touchUnlocked(delta)
}

func (s *NoLockNoEdge) touchUnlocked(delta int) {
	s.value += delta
}
