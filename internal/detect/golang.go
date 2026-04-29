package detect

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// GoFiles discovers every .go file the Go build system would compile under
// srcRoot, honoring build constraints (//go:build), excluding ignored files
// (//go:build ignore) and CGO-conditional alternates the host doesn't pick.
//
// Multi-module: walks for go.mod files (skipping vendor/, node_modules/,
// .git/, and testdata/), then runs packages.Load("./...") in each module
// directory. Returns relpaths from srcRoot in slash form, deduplicated,
// sorted lexicographically.
//
// Tests:true is set so _test.go files included in pkg.GoFiles surface in
// the result — the parser indexes them; omitting them would surface as
// audit drift between the build oracle and what production parses.
//
// When srcRoot contains no go.mod anywhere, returns an empty slice with
// no error (the caller's TS/Sol pipelines may still find files there).
//
// Implementation: thin wrapper over GoPackagesMode(srcRoot, ModeFiles) so
// detect.GoFiles and detect.GoPackages share a single discovery oracle.
// (B1 introduced GoPackages so the Go parser can access types.Info; this
// keeps the file-list contract unchanged.)
func GoFiles(srcRoot string) ([]string, error) {
	pkgs, err := GoPackagesMode(srcRoot, ModeFiles)
	if err != nil {
		return nil, err
	}
	absRoot, _ := filepath.Abs(srcRoot)
	set := map[string]struct{}{}
	for _, pkg := range pkgs {
		for _, abs := range pkg.GoFiles {
			rel, err := filepath.Rel(absRoot, abs)
			if err != nil || strings.HasPrefix(rel, "..") {
				continue
			}
			set[filepath.ToSlash(rel)] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

// GoPackagesLoadMode selects how much info packages.Load is asked to
// produce. ModeFiles is the cheap mode used by GoFiles for discovery only;
// ModeTypes adds full TypesInfo + Syntax (required by the Go concurrency
// pass to resolve sync.Mutex receivers — name-only matching produces
// false positives on user-defined types named "Mutex").
type GoPackagesLoadMode int

const (
	// ModeFiles loads names + file lists only (cheap, GoFiles fingerprint).
	ModeFiles GoPackagesLoadMode = iota
	// ModeTypes loads everything ModeFiles does plus parsed Syntax,
	// resolved Types, and TypesInfo. ~10x slower on large modules; use
	// only when the consumer needs go/types resolution (concurrency pass).
	ModeTypes
)

// GoPackages is shorthand for GoPackagesMode(srcRoot, ModeTypes) — the
// loaded-with-type-info variant. The Go parser uses this to walk Syntax
// trees alongside TypesInfo so concurrency analysis can resolve sync.Mutex
// receivers via types.Object identity rather than fragile name matching.
func GoPackages(srcRoot string) ([]*packages.Package, error) {
	return GoPackagesMode(srcRoot, ModeTypes)
}

// GoPackagesMode runs packages.Load for every go.mod under srcRoot using
// the mode bits selected by `mode`. Returns the union of every module's
// loaded packages in load order. Per-module load failures are returned
// as errors (consistent with GoFiles' fail-fast behavior on Load errors,
// in contrast to per-package type-check failures which are tolerated).
func GoPackagesMode(srcRoot string, mode GoPackagesLoadMode) ([]*packages.Package, error) {
	absRoot, err := filepath.Abs(srcRoot)
	if err != nil {
		return nil, fmt.Errorf("abs srcRoot: %w", err)
	}
	if st, err := os.Stat(absRoot); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("src not a directory: %s", srcRoot)
	}
	modDirs, err := findModuleDirs(absRoot)
	if err != nil {
		return nil, err
	}
	var out []*packages.Package
	for _, modDir := range modDirs {
		pkgs, err := loadModule(modDir, mode)
		if err != nil {
			return nil, err
		}
		out = append(out, pkgs...)
	}
	return out, nil
}

// findModuleDirs walks absRoot for every go.mod, returning the directories
// that contain them. Skips vendor/, node_modules/, .git/, and testdata/ —
// matching `go list ./...` semantics. testdata/ is skipped because Go's own
// pattern resolution excludes it from `./...`; descending into a go.mod
// that lives under testdata/ would import build inputs the parent module
// never sees.
func findModuleDirs(absRoot string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(absRoot, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			n := d.Name()
			if p != absRoot && (n == "vendor" || n == "node_modules" || n == ".git" || n == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "go.mod" {
			dirs = append(dirs, filepath.Dir(p))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk for go.mod: %w", err)
	}
	return dirs, nil
}

// loadModule runs packages.Load("./...") in modDir with the mode bits
// derived from `mode` and returns the resulting packages slice.
//
// Tests:true surfaces _test.go files via test-variant packages. Tests:true
// also synthesizes a `*.test` main package whose generated main file lives
// in the build cache OUTSIDE srcRoot — callers must filter those by relpath
// when projecting back to source-tree paths.
//
// Per-package errors (pkg.Errors) are intentionally NOT propagated: a single
// package failing to type-check should not abort discovery of every other
// module. This mirrors `go list ./...` tolerance and matches audit's prior
// behavior.
func loadModule(modDir string, mode GoPackagesLoadMode) ([]*packages.Package, error) {
	cfg := &packages.Config{
		Mode:  loadModeBits(mode),
		Dir:   modDir,
		Tests: true,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("packages.Load in %s: %w", modDir, err)
	}
	return pkgs, nil
}

// loadModeBits maps the public GoPackagesLoadMode to packages.LoadMode bits.
func loadModeBits(mode GoPackagesLoadMode) packages.LoadMode {
	base := packages.NeedName | packages.NeedFiles | packages.NeedModule
	if mode == ModeTypes {
		base |= packages.NeedSyntax | packages.NeedTypes |
			packages.NeedTypesInfo | packages.NeedImports |
			packages.NeedDeps | packages.NeedCompiledGoFiles
	}
	return base
}
