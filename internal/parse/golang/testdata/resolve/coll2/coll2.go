package coll2

type Other struct{ n int }

// Size is a same-named decoy in a different package; coll1.Set.Quorum must not
// bind to it.
func (o *Other) Size() int { return o.n }
