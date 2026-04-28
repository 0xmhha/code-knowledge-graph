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
func GoFiles(srcRoot string) ([]string, error) {
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

	set := map[string]struct{}{}
	for _, modDir := range modDirs {
		if err := loadModuleFiles(absRoot, modDir, set); err != nil {
			return nil, err
		}
	}

	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
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

// loadModuleFiles runs packages.Load("./...") in modDir and appends every
// pkg.GoFiles entry (after rebasing onto absRoot) into set.
//
// Tests:true surfaces _test.go files via test-variant packages. Tests:true
// also synthesizes a `*.test` main package whose generated main file lives
// in the build cache OUTSIDE srcRoot — the HasPrefix(rel, "..") check below
// filters those (and any stdlib paths) so only files under the user's
// source tree end up in the result.
//
// Per-package errors (pkg.Errors) are intentionally NOT propagated: a single
// package failing to type-check should not abort discovery of every other
// module. This mirrors `go list ./...` tolerance and matches audit's prior
// behavior.
func loadModuleFiles(absRoot, modDir string, set map[string]struct{}) error {
	cfg := &packages.Config{
		Mode:  packages.NeedName | packages.NeedFiles | packages.NeedModule,
		Dir:   modDir,
		Tests: true,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return fmt.Errorf("packages.Load in %s: %w", modDir, err)
	}
	for _, pkg := range pkgs {
		for _, abs := range pkg.GoFiles {
			rel, err := filepath.Rel(absRoot, abs)
			if err != nil || strings.HasPrefix(rel, "..") {
				continue
			}
			set[filepath.ToSlash(rel)] = struct{}{}
		}
	}
	return nil
}
