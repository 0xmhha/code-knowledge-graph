// Package audit compares the authoritative Go build file set
// (go/packages.Load) against the file set recorded in the CKG database.
//
// V0 scope: Go only. TS/Sol audit is deferred (no equivalent build oracle);
// see docs/WORK-PLAN.md Group E.
package audit

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// store is the read surface audit needs from persist.
type store interface {
	DistinctFilePaths(language string) ([]string, error)
}

// Report is the diff between the build's authoritative file set and the
// DB's recorded file set. Slices are sorted for deterministic output.
type Report struct {
	BuildCount  int      `json:"build_count"`
	DBCount     int      `json:"db_count"`
	InBuildOnly []string `json:"in_build_only"` // missing from DB — bug
	InDBOnly    []string `json:"in_db_only"`    // over-included by detect.Walk
	InBoth      int      `json:"in_both"`
}

// IsParity reports whether the build and DB sets are identical.
func (r Report) IsParity() bool {
	return len(r.InBuildOnly) == 0 && len(r.InDBOnly) == 0
}

// RunGo computes the audit report for the Go file set under srcRoot. Build
// set is collected by discovering every go.mod under srcRoot and running
// go/packages.Load("./...") in each module's directory. Files are reported
// as srcRoot-relative slash-separated paths to match how the production
// build pipeline records file_path in the DB.
func RunGo(srcRoot string, s store) (Report, error) {
	buildSet, err := collectBuildSet(srcRoot)
	if err != nil {
		return Report{}, fmt.Errorf("collect build set: %w", err)
	}
	dbPaths, err := s.DistinctFilePaths("go")
	if err != nil {
		return Report{}, fmt.Errorf("query db file_path: %w", err)
	}
	dbSet := make(map[string]struct{}, len(dbPaths))
	for _, p := range dbPaths {
		dbSet[filepath.ToSlash(p)] = struct{}{}
	}
	r := Report{BuildCount: len(buildSet), DBCount: len(dbSet)}
	for p := range buildSet {
		if _, ok := dbSet[p]; ok {
			r.InBoth++
		} else {
			r.InBuildOnly = append(r.InBuildOnly, p)
		}
	}
	for p := range dbSet {
		if _, ok := buildSet[p]; !ok {
			r.InDBOnly = append(r.InDBOnly, p)
		}
	}
	sort.Strings(r.InBuildOnly)
	sort.Strings(r.InDBOnly)
	return r, nil
}

// collectBuildSet finds every go.mod under srcRoot and unions the
// pkg.GoFiles output of go/packages.Load("./...") in each module.
//
// We walk for go.mod files (rather than calling Load once at srcRoot)
// because a corpus may contain multiple modules (testdata/synthetic has
// go-backend/go.mod nested) or none at the root. Load is read-only so
// repeating it per module is safe.
func collectBuildSet(srcRoot string) (map[string]struct{}, error) {
	absRoot, err := filepath.Abs(srcRoot)
	if err != nil {
		return nil, fmt.Errorf("abs srcRoot: %w", err)
	}
	if st, err := os.Stat(absRoot); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("src not a directory: %s", srcRoot)
	}
	out := map[string]struct{}{}
	err = filepath.WalkDir(absRoot, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			n := d.Name()
			if p != absRoot && (n == "vendor" || n == "node_modules" || n == ".git") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "go.mod" {
			return loadModule(absRoot, filepath.Dir(p), out)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk for go.mod: %w", err)
	}
	return out, nil
}

// loadModule runs packages.Load("./...") in modDir and adds GoFiles to set.
//
// Tests:true is set so external test packages (`pkg_test`) and the test
// variants of regular packages surface as their own pkg entries with the
// `_test.go` files in GoFiles. Without this flag, packages.Load excludes
// every `_test.go` file by default — and since detect.Walk's production
// path indexes them by extension, omitting them from the build oracle
// would surface as a 100% spurious DRIFT signal.
func loadModule(absRoot, modDir string, set map[string]struct{}) error {
	cfg := &packages.Config{
		Mode:  packages.NeedName | packages.NeedFiles | packages.NeedModule,
		Dir:   modDir,
		Tests: true,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return fmt.Errorf("packages.Load in %s: %w", modDir, err)
	}
	// Tests:true also synthesizes a `*.test` main package whose generated main
	// file lives in the build cache, OUTSIDE srcRoot. The HasPrefix("..") check
	// below filters those (and stdlib paths reachable via NeedDeps) so only
	// files under the user's source tree end up in the build set.
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

// WriteText emits a human-readable summary plus first-N examples for diffs.
func (r Report) WriteText(w io.Writer) error {
	const previewN = 20
	if _, err := fmt.Fprintf(w, "ckg audit (go)\n  build files: %d\n  db files:    %d\n  in both:     %d\n  in build only (missing from DB): %d\n  in db only (over-included):       %d\n",
		r.BuildCount, r.DBCount, r.InBoth, len(r.InBuildOnly), len(r.InDBOnly)); err != nil {
		return err
	}
	for _, sec := range []struct {
		head  string
		items []string
	}{
		{"MISSING (build → expected in DB)", r.InBuildOnly},
		{"EXTRA (DB has, build does not)", r.InDBOnly},
	} {
		if len(sec.items) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s (%d):\n", sec.head, len(sec.items))
		n := len(sec.items)
		if n > previewN {
			n = previewN
		}
		for i := 0; i < n; i++ {
			fmt.Fprintf(w, "  %s\n", sec.items[i])
		}
		if len(sec.items) > previewN {
			fmt.Fprintf(w, "  ... (%d more)\n", len(sec.items)-previewN)
		}
	}
	verdict := "PARITY"
	if !r.IsParity() {
		verdict = "DRIFT"
	}
	_, err := fmt.Fprintf(w, "verdict: %s\n", verdict)
	return err
}

// WriteJSON emits the report as a single JSON object.
func (r Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
