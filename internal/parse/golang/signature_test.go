package golang

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// TestFormatSignatureFull asserts that formatSignature renders the real
// parameter and result lists (not the historical `func name(...) ...`
// placeholder), and collapses multi-line signatures onto one line. The
// signature is the single most information-dense per-symbol field an analyst
// reads, so it must reveal the actual parameters (e.g. isJustified's
// `targetView View` — the crux of the stale-view fix).
func TestFormatSignatureFull(t *testing.T) {
	src := `package p

type Core struct{}
type Proposal struct{}
type View struct{}

func isJustified(proposal Proposal, targetView View, msgs []*View, quorumSize int) error { return nil }

func (c *Core) handle(x int) (bool, error) { return false, nil }

func noResults(a, b string) {}

func multi(
	a int,
	b string,
) error {
	return nil
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	v := newDeclVisitor(fset, "p.go", "p")

	got := map[string]string{}
	for _, decl := range f.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok {
			got[fd.Name.Name] = v.formatSignature(fd)
		}
	}

	want := map[string]string{
		"isJustified": "func isJustified(proposal Proposal, targetView View, msgs []*View, quorumSize int) error",
		"handle":      "func (Core) handle(x int) (bool, error)",
		"noResults":   "func noResults(a, b string)",
		"multi":       "func multi(a int, b string) error",
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s:\n got  %q\n want %q", name, got[name], w)
		}
	}
}
