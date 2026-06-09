package coll1

type Set struct{ n int }

func (s *Set) Size() int { return s.n }

// Quorum calls the receiver's own Size(). A bare-name resolver could mis-bind
// this to coll2.Other.Size (same method name, different package); the typed
// resolver must bind it to coll1.Set.Size.
func (s *Set) Quorum() int { return s.Size() - 1 }
