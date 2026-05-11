package golang_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	gop "github.com/0xmhha/code-knowledge-graph/internal/parse/golang"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-A minor 4 — unit-level tests for Parser.FuncFieldTouches(). The
// integration tests in internal/buildpipe/lock_propagation_test.go already
// cover the end-to-end (parser → buildpipe → SQLite) path; these tests
// pin down the parser-level contract so a regression in the side-channel
// surface is caught without rebuilding the whole pipeline.
//
// Three properties exercised:
//
//   1. Populated in typed mode — lock-holder fixture functions must
//      appear in the returned map with the expected struct field.
//   2. Deep-copy invariant — mutating the returned map must NOT affect
//      the parser's internal state (regression guard for W-A review
//      Important #1 — `defer Unlock`-then-return-live-map race).
//   3. Empty in AST-only mode — without SetPackages the side-channel
//      must short-circuit (typesInfo is nil, recordFuncFieldTouches
//      no-ops).

// loadLockPropagationFixture is shared setup: walks the lock_propagation
// testdata as a typed Go module and returns the parser with all files
// already through ParseFile. Touches the same code path that production
// buildpipe uses (detect.GoPackages → SetPackages → ParseFile).
//
// Also returns a funcID → qname map built from the per-file ParseResult
// node lists — the parser's funcFieldTouches keys are hashed IDs, so
// callers need the side map to assert against human-readable qnames.
func loadLockPropagationFixture(t *testing.T) (*gop.Parser, map[string]string) {
	t.Helper()
	// W-A fixtures live under internal/buildpipe/testdata/lock_propagation
	// (a sibling tree from this _test.go's package directory).
	root := filepath.Clean(filepath.Join("..", "..", "buildpipe", "testdata", "lock_propagation"))
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedDeps |
			packages.NeedImports | packages.NeedModule | packages.NeedCompiledGoFiles,
		Dir: root,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		t.Fatalf("packages.Load: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatalf("packages.Load returned 0 packages for %s", root)
	}
	p := gop.New(root)
	p.SetPackages(pkgs)
	qnameByFuncID := map[string]string{}
	seen := map[string]struct{}{}
	for _, pkg := range pkgs {
		for _, path := range pkg.GoFiles {
			if _, dup := seen[path]; dup {
				continue
			}
			seen[path] = struct{}{}
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			r, err := p.ParseFile(path, src)
			if err != nil {
				t.Fatalf("ParseFile %s: %v", path, err)
			}
			for _, n := range r.Nodes {
				if n.Type == types.NodeFunction || n.Type == types.NodeMethod {
					qnameByFuncID[n.ID] = n.QualifiedName
				}
			}
		}
	}
	return p, qnameByFuncID
}

// TestFuncFieldTouches_BasicPopulation — at least one lock-holder fixture
// function appears in the side-channel with a non-empty field set. The
// SingleHop fixture's `Apply` method locks mu and calls touch(); the
// touch() helper reads/writes value, so the parser-level map must show
// at least one SingleHop-related entry whose value set contains the value
// field's node ID.
func TestFuncFieldTouches_BasicPopulation(t *testing.T) {
	p, qnameByFuncID := loadLockPropagationFixture(t)
	ft := p.FuncFieldTouches()
	if len(ft) == 0 {
		t.Fatal("FuncFieldTouches empty in typed mode; expected populated map")
	}
	// Find at least one SingleHop-related func in the map. funcID is a
	// content hash; we resolve it back to qname via the side map populated
	// during ParseFile so assertions can target the human-readable name.
	var hits []string
	for funcID := range ft {
		qname := qnameByFuncID[funcID]
		if strings.Contains(qname, "SingleHop") {
			hits = append(hits, funcID)
		}
	}
	if len(hits) == 0 {
		// Surface qnames we DID see so diagnosis is easier on regression.
		seen := make([]string, 0, len(ft))
		for funcID := range ft {
			seen = append(seen, qnameByFuncID[funcID])
		}
		t.Fatalf("no SingleHop-related funcs in FuncFieldTouches; saw qnames=%v", seen)
	}
	// At least one of those hits should touch a non-empty field set.
	nonEmpty := 0
	for _, k := range hits {
		if len(ft[k]) > 0 {
			nonEmpty++
		}
	}
	if nonEmpty == 0 {
		t.Errorf("all SingleHop-related entries (%d) have empty field sets; "+
			"expected at least one touching value", len(hits))
	}
}

// TestFuncFieldTouches_DeepCopy — mutating the returned map must not
// surface in subsequent FuncFieldTouches() calls. Regression guard for
// W-A review Important #1 (commit history: the original implementation
// returned a live reference under `defer Unlock`, which was safe given
// production call ordering but would crack under any re-ordering).
func TestFuncFieldTouches_DeepCopy(t *testing.T) {
	p, _ := loadLockPropagationFixture(t)
	ft1 := p.FuncFieldTouches()
	if len(ft1) == 0 {
		t.Fatal("FuncFieldTouches empty; cannot exercise deep-copy invariant")
	}
	// Mutate the first entry — add a bogus field ID and also replace the
	// inner map with an empty one to cover both shallow + deep mutations.
	var firstKey string
	for k := range ft1 {
		firstKey = k
		break
	}
	ft1[firstKey]["bogus-injected-field"] = struct{}{}
	ft1["bogus-injected-func"] = map[string]struct{}{"x": {}}

	ft2 := p.FuncFieldTouches()
	if _, ok := ft2["bogus-injected-func"]; ok {
		t.Error("outer-map mutation leaked into parser state")
	}
	if _, ok := ft2[firstKey]["bogus-injected-field"]; ok {
		t.Errorf("inner-map mutation leaked into parser state for key=%q", firstKey)
	}
}

// TestFuncFieldTouches_ASTOnlyEmpty — without SetPackages, ParseFile
// falls back to AST-only mode (no typesInfo). The side-channel guards
// on typesInfo != nil at the top of recordFuncFieldTouches, so the map
// must stay empty after parsing the same fixture files.
//
// Why this matters: production buildpipe always passes through typed
// mode (SetPackages from detect.GoPackages), but ad-hoc consumers (tests,
// CLI tools) may parse without it. The W-A propagation pass interprets
// an empty map as "no field-touch data" and silently skips emit — this
// test pins that gating so a future change can't accidentally fire the
// AST-only path through the typed-mode logic.
func TestFuncFieldTouches_ASTOnlyEmpty(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", "buildpipe", "testdata", "lock_propagation"))
	p := gop.New(root)
	// NOTE: intentionally NOT calling SetPackages.
	// Parse just one file — enough to verify the side-channel stays empty.
	target := filepath.Join(root, "single_hop.go")
	src, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read %s: %v", target, err)
	}
	if _, err := p.ParseFile(target, src); err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	ft := p.FuncFieldTouches()
	if len(ft) != 0 {
		t.Errorf("AST-only FuncFieldTouches should be empty; got %d entries", len(ft))
	}
}
