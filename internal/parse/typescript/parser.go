// Package typescript implements the CKG parser for .ts/.tsx/.js/.jsx (spec §4.6.2).
package typescript

import (
	"context"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	js "github.com/smacker/go-tree-sitter/javascript"
	ts "github.com/smacker/go-tree-sitter/typescript/typescript"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
)

// Parser implements parse.Parser for TypeScript / JavaScript source.
type Parser struct {
	srcRoot string
}

// New returns a Parser rooted at srcRoot (used for relative file paths).
func New(srcRoot string) *Parser { return &Parser{srcRoot: srcRoot} }

// Extensions reports the file extensions this parser handles.
func (p *Parser) Extensions() []string {
	return []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"}
}

// ParseFile runs Pass 1 over a single TS/JS source file.
func (p *Parser) ParseFile(path string, src []byte) (*parse.ParseResult, error) {
	rel, err := filepath.Rel(p.srcRoot, path)
	if err != nil {
		rel = path
	}
	parser := sitter.NewParser()
	var lang *sitter.Language
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ts", ".tsx":
		lang = ts.GetLanguage()
	default:
		lang = js.GetLanguage()
	}
	parser.SetLanguage(lang)
	tree, err := parser.ParseCtx(context.Background(), nil, src)
	if err != nil {
		return nil, err
	}
	root := tree.RootNode()
	v := newDeclVisitor(rel, src, lang, root)
	v.visit()
	return &parse.ParseResult{
		Path:    rel,
		Nodes:   v.nodes,
		Edges:   v.edges,
		Pending: v.pending,
	}, nil
}

// Compile-time check that *Parser satisfies parse.Parser.
var _ parse.Parser = (*Parser)(nil)
