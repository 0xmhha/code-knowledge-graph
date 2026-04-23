package parse_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
)

type fakeParser struct{ exts []string }

func (f *fakeParser) ParseFile(string, []byte) (*parse.ParseResult, error) { return nil, nil }
func (f *fakeParser) Resolve([]*parse.ParseResult) (*parse.ResolvedGraph, error) {
	return nil, nil
}
func (f *fakeParser) Extensions() []string { return f.exts }

func TestRegistryDispatch(t *testing.T) {
	r := parse.NewRegistry()
	goP := &fakeParser{exts: []string{".go"}}
	tsP := &fakeParser{exts: []string{".ts", ".tsx"}}
	if err := r.Register(goP); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(tsP); err != nil {
		t.Fatal(err)
	}
	if r.For("foo.go") != goP {
		t.Error(".go should dispatch to goP")
	}
	if r.For("bar.tsx") != tsP {
		t.Error(".tsx should dispatch to tsP")
	}
	if r.For("baz.py") != nil {
		t.Error(".py should be unregistered")
	}
}

func TestRegistryDuplicateExtension(t *testing.T) {
	r := parse.NewRegistry()
	if err := r.Register(&fakeParser{exts: []string{".go"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(&fakeParser{exts: []string{".go"}}); err == nil {
		t.Error("duplicate extension should fail")
	}
}
