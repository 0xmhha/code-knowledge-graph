package server

import (
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// computeStaleness compares the manifest's recorded SrcCommit against a live
// git lookup of m.SrcRoot. Returns ("", false) when the manifest was not
// git-fingerprinted, or when git lookup fails — in that case the viewer
// simply won't show a stale banner.
//
// When m.SrcRelPath is set (manifests written by the path-aware pipeline) the
// live commit is `git -C <repoRoot> log -1 --format=%H -- <relPath>` so
// unrelated commits elsewhere in a monorepo do not flip staleness.
//
// When m.SrcRelPath is empty (legacy manifests written before this change)
// the implementation falls back to `git -C srcRoot rev-parse HEAD` to keep
// existing /tmp/ckg-* graphs working — but that path retains the legacy
// false-positive behaviour for sub-directory builds.
func computeStaleness(m persist.Manifest) (current string, stale bool) {
	if m.StalenessMethod != "git" || m.SrcRoot == "" {
		return "", false
	}
	if m.SrcRelPath == "" {
		// Legacy branch: manifests written before SrcRelPath existed compare
		// the whole-repo HEAD via `git -C srcRoot rev-parse HEAD`. Retained
		// so existing /tmp/ckg-* graphs still resolve to *some* answer
		// rather than silently disabling the stale banner — but this path
		// keeps the documented false-positive behaviour for sub-directory
		// builds. TestComputeStaleness_LegacyBackcompat_DiscriminatesPathAware
		// pins this branch in place.
		out, err := exec.Command("git", "-C", m.SrcRoot, "rev-parse", "HEAD").Output()
		if err != nil {
			return "", false
		}
		current = strings.TrimSpace(string(out))
		return current, current != m.SrcCommit
	}
	repoRoot, err := repoToplevel(m.SrcRoot)
	if err != nil {
		return "", false
	}
	out, err := exec.Command("git", "-C", repoRoot, "log", "-1", "--format=%H", "--", m.SrcRelPath).Output()
	if err != nil {
		return "", false
	}
	current = strings.TrimSpace(string(out))
	if current == "" {
		return "", false
	}
	return current, current != m.SrcCommit
}

// repoToplevel returns the absolute path of the git repository that contains
// srcRoot. Errors propagate so the caller can disable staleness rather than
// silently comparing against a wrong root.
//
// The returned path is resolved through filepath.EvalSymlinks so a later
// `git -C <root> log -- <relPath>` agrees on identity with paths recorded
// at build time on platforms (macOS) where /tmp links into /private/tmp.
func repoToplevel(srcRoot string) (string, error) {
	out, err := exec.Command("git", "-C", srcRoot, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(string(out))
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return filepath.Clean(root), nil
}
