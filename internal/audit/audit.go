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
	"path/filepath"
	"sort"

	"github.com/0xmhha/code-knowledge-graph/internal/detect"
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
	InDBOnly    []string `json:"in_db_only"`    // over-included relative to build oracle
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

// collectBuildSet delegates to detect.GoFiles — the single source of truth
// for "what is a Go build file under srcRoot". Audit and the production
// build pipeline must agree on this set; sharing the implementation makes
// drift between them structurally impossible.
//
// Returns a set form (map keyed by slash-rel-path) for fast lookup against
// the DB set during diff computation.
func collectBuildSet(srcRoot string) (map[string]struct{}, error) {
	files, err := detect.GoFiles(srcRoot)
	if err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(files))
	for _, p := range files {
		out[p] = struct{}{}
	}
	return out, nil
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
		_, _ = fmt.Fprintf(w, "\n%s (%d):\n", sec.head, len(sec.items))
		n := min(len(sec.items), previewN)
		for i := range n {
			_, _ = fmt.Fprintf(w, "  %s\n", sec.items[i])
		}
		if len(sec.items) > previewN {
			_, _ = fmt.Fprintf(w, "  ... (%d more)\n", len(sec.items)-previewN)
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
