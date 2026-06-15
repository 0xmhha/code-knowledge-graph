package coll1

// Hasher is an interface whose method name (Hash) collides with an unrelated
// concrete type's method below. An interface-dispatch call must resolve to the
// interface's method, not be bare-name-bound to the concrete one.
type Hasher interface {
	Hash() string
}

// Thing is an unrelated concrete type that also has a Hash method — the
// distractor for the bare-name resolver.
type Thing struct{}

func (t Thing) Hash() string { return "thing" }

// UseHasher calls the interface method. The resulting invokes/calls edge must
// point at coll1.Hasher.Hash, never coll1.Thing.Hash.
func UseHasher(h Hasher) string { return h.Hash() }

// counter has a method literally named "len" — the distractor for the builtin
// len() call below.
type counter struct{ items []int }

func (c counter) len() int { return len(c.items) }

// CountBuiltin calls the builtin len(). It must NOT produce a call edge to the
// coll1.counter.len method (builtins have no graph node).
func CountBuiltin(xs []int) int { return len(xs) }
