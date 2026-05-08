// Package usestype_fixture exercises Track C's three new detectors at once:
//   - uses_type   (P0)  : Function/Method/Struct → Type
//   - invokes     (P1b) : interface dispatch / func value / closure
//   - instantiates (P1c): composite literal / new(T)
//
// See uses_type_test.go (in the parent gop package) for assertions.
package usestype_fixture

// Config is a struct used as a parameter / field / return type to exercise
// uses_type emission across granularities.
type Config struct {
	Name    string
	Counter *Counter
}

// Counter is referenced as a *Config field — exercises the "field type with
// pointer indirection" branch of forEachNamedTypeInType.
type Counter struct {
	N int
}

// Result is the return type — exercises the result-side of uses_type.
type Result struct {
	OK bool
}

// Logger is an interface used only as a parameter — exercises uses_type
// when the parameter type is a named interface (no implements relation
// applies because there's no concrete satisfier in this fixture).
type Logger interface {
	Log(msg string)
}

// Process takes a Config and a Logger and returns a Result — three uses_type
// edges expected:
//
//	Process uses_type Config
//	Process uses_type Logger
//	Process uses_type Result
//
// And one invokes (interface_method) edge expected from `l.Log(...)`.
//
// And one instantiates edge expected from `Result{OK: true}`.
func Process(c *Config, l Logger) Result {
	l.Log(c.Name)
	return Result{OK: true}
}

// MakeCounter exercises the new(T) instantiation path — exactly one
// instantiates edge to Counter, plus one uses_type edge to *Counter.
func MakeCounter() *Counter {
	return new(Counter)
}

// invokeClosure exercises the closure literal call path — one invokes edge
// with dispatch_kind="closure".
func invokeClosure() int {
	return func() int { return 42 }()
}

// CallbackHolder stores a function value; FireCallback dispatches via the
// stored value — one invokes edge with dispatch_kind="method_value".
type CallbackHolder struct {
	cb func(int) int
}

func (h *CallbackHolder) FireCallback(x int) int {
	return h.cb(x)
}

// callFuncValue exercises the func_value dispatch path — one invokes edge
// with dispatch_kind="func_value".
func callFuncValue(fn func(string) string) string {
	return fn("hello")
}
