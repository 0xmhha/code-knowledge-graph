// Package buildpipe — staleness.go computes the manifest's staleness
// fingerprint. Prefers a path-aware git lookup (so unrelated commits don't
// flip the banner — see internal/server/staleness.go for the symmetrical
// serve-side comparison); falls back to mtime sum of up to 5 detected files
// when the source root isn't a git checkout. Extracted from pipeline.go in
// G4. Pure file move — no behavior change.
package buildpipe

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/0xmhha/code-knowledge-graph/internal/detect"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// setStaleness records the staleness fingerprint on the manifest. Prefers a
// path-aware git commit SHA (the last commit that modified files under
// m.SrcRoot); falls back to summing mtimes of up to 5 detected files when the
// source root is not a git checkout or has no commit history yet.
//
// The path-aware lookup is required for sub-directories of larger repos
// (monorepos, or tools' own testdata): a plain `git rev-parse HEAD` would
// flip on every unrelated commit elsewhere in the repo, producing
// false-positive stale banners in the viewer.
func setStaleness(m *persist.Manifest, log *slog.Logger) {
	repoRoot, relPath, ok := gitRepoRel(m.SrcRoot)
	if ok {
		commit, err := pathAwareHead(repoRoot, relPath)
		if err == nil && commit != "" {
			m.SrcCommit = commit
			m.SrcRelPath = relPath
			m.StalenessMethod = "git"
			return
		}
		if err != nil {
			log.Warn("path-aware git HEAD failed; falling back to mtime",
				"repo", repoRoot, "rel", relPath, "err", err)
		}
	}
	m.StalenessMethod = "mtime"
	// Mtime fallback only needs a stable, deterministic subset of source
	// files — it doesn't need to mirror the build oracle exactly. detect.Walk
	// is intentionally reused here (rather than detect.GoFiles + the TS/Sol
	// halves of detect.Walk) because the cost of forking another packages.Load
	// for fingerprinting outweighs the cost of a few build-tag-excluded paths
	// landing in the StalenessFiles list — the resulting hash is still
	// deterministic and that's all this codepath needs.
	files, _ := detect.Walk(m.SrcRoot)
	all := append(append([]string{}, files.Go...), files.TS...)
	all = append(all, files.Sol...)
	if len(all) > 5 {
		all = all[:5]
	}
	var sum int64
	for _, rel := range all {
		st, err := os.Stat(filepath.Join(m.SrcRoot, rel))
		if err == nil {
			sum += st.ModTime().UnixNano()
		}
	}
	m.StalenessFiles = all
	m.StalenessMTimeSum = sum
}

// gitRepoRel returns (repoRoot, relPathFromRepoRoot, true) when srcRoot lives
// inside a git checkout, or ("", "", false) otherwise. relPath is normalised
// to forward slashes and is "." when srcRoot is the repo root itself.
//
// Both repoRoot and srcRoot are resolved through filepath.EvalSymlinks before
// computing filepath.Rel — without this, on macOS where /tmp -> /private/tmp
// the rel computation walks ../../../ across the symlink boundary and yields
// a path git cannot resolve.
func gitRepoRel(srcRoot string) (string, string, bool) {
	out, err := exec.Command("git", "-C", srcRoot, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", "", false
	}
	repoRoot := strings.TrimSpace(string(out))
	if repoRoot == "" {
		return "", "", false
	}
	absSrc, err := filepath.Abs(srcRoot)
	if err != nil {
		return "", "", false
	}
	if resolved, err := filepath.EvalSymlinks(absSrc); err == nil {
		absSrc = resolved
	}
	if resolved, err := filepath.EvalSymlinks(repoRoot); err == nil {
		repoRoot = resolved
	}
	rel, err := filepath.Rel(repoRoot, absSrc)
	if err != nil {
		return "", "", false
	}
	return repoRoot, filepath.ToSlash(rel), true
}

// pathAwareHead runs `git -C <repoRoot> log -1 --format=%H -- <relPath>` and
// returns the trimmed SHA. Empty output (no commit ever touched relPath) is
// returned as ("", nil) so callers can fall back to mtime fingerprinting.
func pathAwareHead(repoRoot, relPath string) (string, error) {
	out, err := exec.Command("git", "-C", repoRoot, "log", "-1", "--format=%H", "--", relPath).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
