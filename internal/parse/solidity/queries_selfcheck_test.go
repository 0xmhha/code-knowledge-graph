package solidity

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"

	solang "github.com/0xmhha/code-knowledge-graph/internal/parse/solidity/binding"
)

// TestDetectorQueriesCompile compiles every tree-sitter query string in the
// package against the vendored Solidity grammar.
//
// Detectors compile their queries at runtime and silently skip on failure
// (`if qErr != nil { return }`), so a query that breaks against a grammar bump
// would drop an entire edge class with no error — directly degrading
// keyword-retrieval accuracy. This test turns that into a loud CI failure.
//
// Queries are discovered by scanning the package source (go/ast) for raw
// string constants that look like tree-sitter queries, so newly added queries
// — inline or package-level — are covered automatically with no registry to
// keep in sync.
func TestDetectorQueriesCompile(t *testing.T) {
	queries := discoverQueries(t, ".")
	if len(queries) == 0 {
		t.Fatal("no tree-sitter queries discovered — the scanner heuristic is broken")
	}

	lang := solang.GetLanguage()
	for q, pos := range queries {
		query, qErr := sitter.NewQuery(lang, q)
		if qErr != nil {
			t.Errorf("query at %s failed to compile against the grammar: %v\n  query: %s", pos, qErr, q)
			continue
		}
		query.Close()
	}
	t.Logf("compiled %d distinct detector queries", len(queries))
}

// TestQueryCompileDetectsFailure is a guard on the guard: it confirms the
// grammar actually rejects an invalid query, so a green TestDetectorQueriesCompile
// means "all queries compile" rather than "NewQuery never errors". The bad
// query lives in this _test.go file, which discoverQueries excludes.
func TestQueryCompileDetectsFailure(t *testing.T) {
	const bad = `(no_such_node_type) @x`
	lang := solang.GetLanguage()
	if _, qErr := sitter.NewQuery(lang, bad); qErr == nil {
		t.Fatal("expected the grammar to reject a query over an unknown node type")
	}
}

// discoverQueries returns every distinct tree-sitter query string defined in
// the non-test Go sources under dir, mapped to its "file:line" origin.
func discoverQueries(t *testing.T, dir string) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package source: %v", err)
	}

	out := map[string]string{}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				val, err := strconv.Unquote(lit.Value)
				if err != nil || !looksLikeQuery(val) {
					return true
				}
				if _, dup := out[val]; !dup {
					out[val] = fmt.Sprintf("%s:%d", name, fset.Position(lit.Pos()).Line)
				}
				return true
			})
		}
	}
	return out
}

// looksLikeQuery heuristically identifies a tree-sitter query string: every
// detector query in this package uses at least one `@capture` and is an
// S-expression or alternation, which ordinary string constants are not.
func looksLikeQuery(s string) bool {
	t := strings.TrimSpace(s)
	if !strings.Contains(t, "@") {
		return false
	}
	return strings.HasPrefix(t, "(") || strings.HasPrefix(t, "[")
}
