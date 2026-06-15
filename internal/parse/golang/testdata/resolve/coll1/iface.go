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

// --- promoted-method fixture (defect C) ---

// Base has a method that an embedding type should promote.
type Base struct{}

func (b Base) Ping() string { return "pong" }

// Derived embeds Base and therefore promotes Ping; the graph should carry a
// coll1.Derived.Ping method node pointing at Base.Ping's implementation.
type Derived struct {
	Base
}

// --- field-write fixture (defect E) ---

// Box has a field that setBox writes and getBox only reads.
type Box struct{ Val int }

func setBox(b *Box, n int) { b.Val = n } // writes coll1.Box.Val

func getBox(b *Box) int { return b.Val } // reads only — no writes_field
