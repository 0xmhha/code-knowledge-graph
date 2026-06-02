// Package typescript implements the CKG parser for .ts/.tsx/.js/.jsx (spec §4.6.2).
package typescript

import (
	"fmt"
	"path/filepath"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tsjs "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tsts "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

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
	defer func() { parser.Close() }()
	lang := languageForExt(filepath.Ext(path))
	if err := parser.SetLanguage(lang); err != nil {
		return nil, fmt.Errorf("typescript: SetLanguage: %w", err)
	}
	tree := parser.Parse(src, nil)
	if tree == nil {
		return nil, fmt.Errorf("typescript: parser returned nil tree for %s", rel)
	}
	defer func() { tree.Close() }()
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

// languageForExt returns the upstream tree-sitter Language for the given file
// extension. .ts/.tsx use the typescript grammar (TSX is a superset); .js and
// friends use the javascript grammar. Caching is unnecessary: NewLanguage just
// wraps a static C pointer.
func languageForExt(ext string) *sitter.Language {
	switch strings.ToLower(ext) {
	case ".ts":
		return sitter.NewLanguage(tsts.LanguageTypescript())
	case ".tsx":
		return sitter.NewLanguage(tsts.LanguageTSX())
	default:
		return sitter.NewLanguage(tsjs.Language())
	}
}

// Compile-time check that *Parser satisfies parse.Parser.
var _ parse.Parser = (*Parser)(nil)
