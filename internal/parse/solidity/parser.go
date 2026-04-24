// Package solidity implements the CKG parser for .sol files (spec §4.6.3).
//
// We use a vendored copy of github.com/JoranHonig/tree-sitter-solidity v1.2.11
// (smacker/go-tree-sitter does not ship a `solidity` subpackage, and v1.2.11
// is the newest tag whose parser ABI version is ≤14 — the maximum supported
// by smacker's bundled tree-sitter runtime).
package solidity

import (
	"context"
	"path/filepath"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	solang "github.com/0xmhha/code-knowledge-graph/internal/parse/solidity/binding"
)

// Parser implements parse.Parser for Solidity source.
//
// abi accumulates per-contract function signatures across ParseFile calls
// so that the cross-language linker (T20) can match them to TypeScript
// classes by name. Mutated in place from declVisitor.collectABI; safe under
// the dispatcher's per-language sequential Resolve pass.
type Parser struct {
	srcRoot string
	abi     map[string][]ABISig
}

// New returns a Parser rooted at srcRoot (used for relative file paths).
func New(srcRoot string) *Parser {
	return &Parser{srcRoot: srcRoot, abi: map[string][]ABISig{}}
}

// Extensions reports the file extensions this parser handles.
func (p *Parser) Extensions() []string { return []string{".sol"} }

// ParseFile runs Pass 1 over a single .sol file.
func (p *Parser) ParseFile(path string, src []byte) (*parse.ParseResult, error) {
	rel, err := filepath.Rel(p.srcRoot, path)
	if err != nil {
		rel = path
	}
	lang := solang.GetLanguage()
	parser := sitter.NewParser()
	parser.SetLanguage(lang)
	tree, err := parser.ParseCtx(context.Background(), nil, src)
	if err != nil {
		return nil, err
	}
	v := newDeclVisitor(rel, src, lang, tree.RootNode(), p.abi)
	v.visit()
	return &parse.ParseResult{
		Path:    rel,
		Nodes:   v.nodes,
		Edges:   v.edges,
		Pending: v.pending,
	}, nil
}

// ABI returns the per-contract signatures collected during ParseFile.
// Used by the cross-language linker (T20).
func (p *Parser) ABI() map[string][]ABISig { return p.abi }

// Compile-time check that *Parser satisfies parse.Parser.
var _ parse.Parser = (*Parser)(nil)
