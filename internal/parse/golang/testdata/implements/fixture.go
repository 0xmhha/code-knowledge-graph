// Package implements_fixture exercises the P0 implements / extends emission
// pass. See implements_test.go for assertions.
//
// Coverage matrix:
//   - Greeter is an interface with one method.
//   - Hello satisfies Greeter via a value-receiver method (T's method set).
//   - World satisfies Greeter only via a pointer-receiver method (*T's).
//     Both T and *T receiver shapes must be checked for full satisfaction.
//   - Doer has no Greet method — must NOT produce an implements edge.
//   - Goodbye is a small interface embedded into Closer; the pass should
//     emit an extends edge Closer → Goodbye.
//   - Anything (interface{}) is the empty interface — every type satisfies
//     it, so the implements pass must NOT emit edges into it.
package implements_fixture

// Greeter is satisfied by Hello (value receiver) and World (pointer receiver).
type Greeter interface {
	Greet() string
}

// Hello implements Greeter with a value-receiver method — both Hello and
// *Hello have Greet() in their method set.
type Hello struct{}

func (h Hello) Greet() string { return "hi" }

// World implements Greeter only via a pointer-receiver method — only *World
// has Greet() in its method set, World does not. The pass must check both
// shapes (T, *T) to catch this case.
type World struct{}

func (w *World) Greet() string { return "world" }

// Doer has no Greet method — it must NOT appear as the src of any
// implements edge with Greeter as dst.
type Doer struct{}

func (d Doer) Do() {}

// Goodbye is embedded into Closer below — exercises the extends path.
type Goodbye interface {
	Bye() string
}

// Closer embeds Goodbye and adds Close(). The pass must emit:
//
//	extends(Closer -> Goodbye)
//
// AND must NOT emit implements(Closer -> Goodbye) — interface→interface
// relationships are extends, never implements.
type Closer interface {
	Goodbye
	Close() error
}

// Anything is the empty interface — every type satisfies it. The pass
// must skip it explicitly to avoid emitting a low-signal edge from every
// concrete type into Anything.
type Anything interface{}
