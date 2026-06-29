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
	"strings"
	"sync"

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
	// pkgs retains the loaded packages slice so post-Pass-2 phases (e.g.
	// EmitImplementsEdges) can iterate package scopes. Same lifetime as
	// fileIndex — both populated by SetPackages, both nil in AST-only mode.
	pkgs []*packages.Package
	// funcFieldTouchesMu guards funcFieldTouches across the parallel
	// parseConcurrent worker pool. ParseFile is called from many goroutines;
	// each worker merges its declVisitor's per-file touches into the parser
	// map under this lock. Read access (FuncFieldTouches) happens after all
	// workers finish, so a read-write mutex isn't needed.
	funcFieldTouchesMu sync.Mutex
	// funcFieldTouches aggregates per-function field-access sets across every
	// file ParseFile sees. Keyed by Function/Method node ID, value is the set
	// of struct-Field node IDs whose values are read or written in the body.
	// Used by buildpipe's W-A cross-function lock propagation pass (opt-in
	// via --lock-propagation).
	//
	// Lifetime: populated by ParseFile, read by FuncFieldTouches() after the
	// parser is done. Idempotent across Resolve calls — the map is append-only
	// per (funcID, fieldID) pair.
	funcFieldTouches map[string]map[string]struct{}
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
		p.pkgs = nil
		return
	}
	p.fileIndex = buildFileIndex(pkgs)
	p.pkgs = pkgs
}

// Pkgs returns the loaded packages slice registered by SetPackages, or nil
// when the parser is in AST-only mode. Consumers (e.g. the implements pass)
// use this to iterate package scopes after Pass 2 Resolve has run. The slice
// is the live value — callers must not mutate it.
func (p *Parser) Pkgs() []*packages.Package { return p.pkgs }

// isTestVariantPkg reports whether pkg is a go/packages test variant rather
// than a primary build package. With Tests:true, packages.Load returns, for a
// package P, both P (the `make gstable`/`go build` compile set) and the test
// variants P [P.test] (re-includes P's production files plus its internal
// _test.go), P_test [P.test] (external test files), and a synthesized P.test
// main. The variant IDs carry `.test]` or a trailing `.test`; the primary's ID
// does not. See ADR-0002.
func isTestVariantPkg(pkg *packages.Package) bool {
	return strings.Contains(pkg.ID, ".test]") || strings.HasSuffix(pkg.ID, ".test")
}

// buildFileIndex flattens a slice of packages into one (path → typedFile) map
// with DETERMINISTIC, order-independent ownership (ADR-0002). The previous
// first-seen-wins dedup let packages.Load's unstable order decide which variant
// owned a shared production file — measured at 17.5% of production files landing
// on a test variant, which resolves their symbols under the wrong package
// context (unstable/empty canonical_id).
//
// Stage 1: primary (non-test-variant) packages own every production file. Stage
// 2: test variants then fill only files no primary package compiled — i.e. the
// _test.go files — preserving test code as few-shot context without overriding
// the production core.
func buildFileIndex(pkgs []*packages.Package) map[string]typedFile {
	idx := map[string]typedFile{}
	add := func(wantTestVariant bool) {
		for _, pkg := range pkgs {
			if pkg == nil || pkg.TypesInfo == nil || pkg.Fset == nil {
				continue
			}
			if isTestVariantPkg(pkg) != wantTestVariant {
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
	}
	add(false) // primary packages own production files
	add(true)  // test variants fill only the remaining _test.go files
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
		// E3 (G5 Distributed): runs AFTER ast.Walk so v.nodes already contains
		// the Function/Method node IDs we resolve handler / RPC-target args
		// against. Idempotent within a single file.
		v.emitDistributedDecls(tf.file)
		// P2 (G3 control-flow context propagation): runs AFTER
		// emitDistributedDecls so the same v.nodes function-ID lookup is
		// usable. Self-loop edges only — never produces new nodes.
		v.emitContextPaths(tf.file)
		p.mergeFuncFieldTouches(v.funcFieldTouches)
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
	v.emitDistributedDecls(f)
	v.emitContextPaths(f)
	p.mergeFuncFieldTouches(v.funcFieldTouches)
	return &parse.ParseResult{
		Path: rel, Nodes: v.nodes, Edges: v.edges, Pending: v.pending,
	}, nil
}

// mergeFuncFieldTouches merges a single declVisitor's per-file
// funcFieldTouches into the parser-wide aggregate. Thread-safe — callable
// from concurrent ParseFile workers in parseConcurrent. No-op when src is
// empty (AST-only mode or files with no field accesses).
func (p *Parser) mergeFuncFieldTouches(src map[string]map[string]struct{}) {
	if len(src) == 0 {
		return
	}
	p.funcFieldTouchesMu.Lock()
	defer p.funcFieldTouchesMu.Unlock()
	if p.funcFieldTouches == nil {
		p.funcFieldTouches = make(map[string]map[string]struct{}, len(src))
	}
	for funcID, fields := range src {
		dst := p.funcFieldTouches[funcID]
		if dst == nil {
			dst = make(map[string]struct{}, len(fields))
			p.funcFieldTouches[funcID] = dst
		}
		for fid := range fields {
			dst[fid] = struct{}{}
		}
	}
}

// FuncFieldTouches returns the parser-wide map of Function/Method node ID
// → set of struct-Field node IDs touched by the body. Populated during
// ParseFile when typesInfo is available; empty otherwise.
//
// Consumed by buildpipe's W-A cross-function lock propagation pass.
//
// Returns a deep copy of the internal map so the parser's worker pool can
// continue mutating its own state without risking a data race with the
// caller. W-A review (2026-05-11 Important #1) caught that the prior
// implementation `defer Unlock` then returned the live map reference —
// safe today because runGoPipeline only calls this after parseConcurrent
// completes, but a single re-ordering would surface a silent race. The
// copy here is O(funcs × fields_touched), measured at ≪1 ms on the
// CKG self-graph (15 lock-holders).
func (p *Parser) FuncFieldTouches() map[string]map[string]struct{} {
	p.funcFieldTouchesMu.Lock()
	defer p.funcFieldTouchesMu.Unlock()
	out := make(map[string]map[string]struct{}, len(p.funcFieldTouches))
	for fn, fields := range p.funcFieldTouches {
		cp := make(map[string]struct{}, len(fields))
		for f := range fields {
			cp[f] = struct{}{}
		}
		out[fn] = cp
	}
	return out
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
