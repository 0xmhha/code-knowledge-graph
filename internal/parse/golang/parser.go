// Package golang is the Go-language parser for CKG. It uses go/parser +
// go/types via golang.org/x/tools/go/packages to extract declarations and
// resolved cross-file references (spec §4.6.1).
package golang

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
)

// Parser implements parse.Parser for Go source.
type Parser struct {
	srcRoot string
	fset    *token.FileSet
}

// New returns a Parser rooted at srcRoot (used for relative file paths).
func New(srcRoot string) *Parser {
	return &Parser{srcRoot: srcRoot, fset: token.NewFileSet()}
}

func (p *Parser) Extensions() []string { return []string{".go"} }

// ParseFile runs Pass 1: structural extraction. It does NOT resolve
// cross-file references — those become PendingRefs handled in Resolve.
func (p *Parser) ParseFile(path string, src []byte) (*parse.ParseResult, error) {
	rel, err := filepath.Rel(p.srcRoot, path)
	if err != nil {
		rel = path
	}
	f, err := parser.ParseFile(p.fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	v := newDeclVisitor(p.fset, rel, f.Name.Name)
	ast.Walk(v, f)
	return &parse.ParseResult{
		Path:    rel,
		Nodes:   v.nodes,
		Edges:   v.edges,
		Pending: v.pending,
	}, nil
}

// Resolve is implemented in resolve.go (Task 9).

// Compile-time check that *Parser satisfies parse.Parser.
var _ parse.Parser = (*Parser)(nil)
