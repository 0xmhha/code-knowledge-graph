// Package golang is the Go-language parser for CKG. It uses go/parser +
// go/types via golang.org/x/tools/go/packages to extract declarations and
// resolved cross-file references (spec §4.6.1).
package golang

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"

	"golang.org/x/tools/go/packages"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
)

// Parser implements parse.Parser for Go source.
//
// Two operating modes:
//   - "AST-only" (default): ParseFile re-parses the file with go/parser. No
//     types.Info is available, so the concurrency pass falls back to
//     name-based heuristics with INFERRED confidence. Maintains backward
//     compatibility for callers that don't have a *packages.Package handy
//     (existing tests, ad-hoc CLI use).
//   - "Type-aware": SetPackages() registers a pre-loaded []*packages.Package
//     (from detect.GoPackages). ParseFile then locates the file in the loaded
//     syntax trees and uses the matching *types.Info for receiver resolution
//     in the concurrency pass — emitting Mutex / Lock edges with EXTRACTED
//     confidence and zero false positives on user-defined "Mutex" types.
type Parser struct {
	srcRoot string
	fset    *token.FileSet
	// fileIndex: absolute file path → (TypesInfo, *ast.File) loaded via
	// packages.Load. Populated by SetPackages; nil when AST-only mode.
	fileIndex map[string]typedFile
}

// typedFile holds the parsed AST + resolved type info for one source file,
// loaded by go/packages. Both pointers are nil-safe — callers must check
// before dereferencing.
type typedFile struct {
	info *types.Info
	file *ast.File
	fset *token.FileSet
}

// New returns a Parser rooted at srcRoot (used for relative file paths).
func New(srcRoot string) *Parser {
	return &Parser{srcRoot: srcRoot, fset: token.NewFileSet()}
}

// SetPackages registers pre-loaded packages so subsequent ParseFile calls
// can use go/types resolution. Must be called BEFORE ParseFile for the
// type-aware path to take effect; idempotent — subsequent calls overwrite
// the index. Pass nil/empty to revert to AST-only mode.
func (p *Parser) SetPackages(pkgs []*packages.Package) {
	if len(pkgs) == 0 {
		p.fileIndex = nil
		return
	}
	p.fileIndex = buildFileIndex(pkgs)
}

// buildFileIndex flattens a slice of packages into one (path → typedFile)
// map. When the same file appears in multiple package variants (base + test),
// the first-seen entry wins (tests carry the same TypesInfo for shared
// production files; the differentiating data is in the test-only files).
func buildFileIndex(pkgs []*packages.Package) map[string]typedFile {
	idx := map[string]typedFile{}
	for _, pkg := range pkgs {
		if pkg == nil || pkg.TypesInfo == nil || pkg.Fset == nil {
			continue
		}
		for i, f := range pkg.Syntax {
			if f == nil || i >= len(pkg.CompiledGoFiles) {
				continue
			}
			path := pkg.CompiledGoFiles[i]
			if _, exists := idx[path]; exists {
				continue
			}
			idx[path] = typedFile{info: pkg.TypesInfo, file: f, fset: pkg.Fset}
		}
	}
	return idx
}

func (p *Parser) Extensions() []string { return []string{".go"} }

// ParseFile runs Pass 1: structural extraction. It does NOT resolve
// cross-file references — those become PendingRefs handled in Resolve.
//
// When a *packages.Package was registered for `path` via SetPackages,
// uses the pre-parsed AST + TypesInfo (concurrency pass becomes EXTRACTED).
// Otherwise re-parses with go/parser (concurrency pass falls back to
// name-only heuristics with INFERRED confidence).
func (p *Parser) ParseFile(path string, src []byte) (*parse.ParseResult, error) {
	rel, err := filepath.Rel(p.srcRoot, path)
	if err != nil {
		rel = path
	}
	if tf, ok := p.lookupTyped(path); ok {
		v := newDeclVisitor(tf.fset, rel, tf.file.Name.Name)
		v.typesInfo = tf.info
		// Mutex nodes must be emitted BEFORE the body walk so the Lock/Unlock
		// detector (in statements.go's CallExpr case) can resolve receivers
		// to the matching Mutex node ID. Channel emission stays inline with
		// the body walk because each `make(chan T)` is its own self-contained
		// node that doesn't need a pre-walk index.
		v.emitConcurrencyDecls(tf.file)
		ast.Walk(v, tf.file)
		return &parse.ParseResult{
			Path: rel, Nodes: v.nodes, Edges: v.edges, Pending: v.pending,
		}, nil
	}
	f, err := parser.ParseFile(p.fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	v := newDeclVisitor(p.fset, rel, f.Name.Name)
	v.emitConcurrencyDecls(f)
	ast.Walk(v, f)
	return &parse.ParseResult{
		Path: rel, Nodes: v.nodes, Edges: v.edges, Pending: v.pending,
	}, nil
}

// lookupTyped returns the registered typedFile for path. Tries an exact
// match first; if that fails, falls back to a basename + abs-match scan
// because go/packages may report paths via /private/tmp/ symlinks on macOS
// while the caller passed /tmp/. Returns false when no match is found.
func (p *Parser) lookupTyped(path string) (typedFile, bool) {
	if p.fileIndex == nil {
		return typedFile{}, false
	}
	if tf, ok := p.fileIndex[path]; ok {
		return tf, true
	}
	if abs, err := filepath.Abs(path); err == nil {
		if tf, ok := p.fileIndex[abs]; ok {
			return tf, true
		}
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			if tf, ok := p.fileIndex[resolved]; ok {
				return tf, true
			}
		}
	}
	return typedFile{}, false
}

// Resolve is implemented in resolve.go (Task 9).

// Compile-time check that *Parser satisfies parse.Parser.
var _ parse.Parser = (*Parser)(nil)
