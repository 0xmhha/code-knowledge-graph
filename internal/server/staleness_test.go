package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// TestComputeStaleness_PathAware drives computeStaleness through scenarios
// that simulate the build → serve handoff: a manifest is constructed with a
// recorded SrcCommit + SrcRelPath, then the underlying repo is mutated and
// the live staleness check is asserted.
func TestComputeStaleness_PathAware(t *testing.T) {
	cases := []struct {
		name             string
		repoMutation     func(t *testing.T, repo string) // happens AFTER initial commit
		srcSubdir        string
		expectStale      bool
		expectCurrentNon bool // current commit must be non-empty
	}{
		{
			name:             "no further commits → not stale",
			repoMutation:     func(t *testing.T, repo string) {},
			srcSubdir:        "sub",
			expectStale:      false,
			expectCurrentNon: true,
		},
		{
			name: "commit OUTSIDE src_root → not stale (the bug we fixed)",
			repoMutation: func(t *testing.T, repo string) {
				commitFileTo(t, repo, "other/b.go", "package other\n", "outside change")
			},
			srcSubdir:        "sub",
			expectStale:      false,
			expectCurrentNon: true,
		},
		{
			name: "commit INSIDE src_root → stale",
			repoMutation: func(t *testing.T, repo string) {
				commitFileTo(t, repo, "sub/a.go", "package sub\n// edited\n", "inside change")
			},
			srcSubdir:        "sub",
			expectStale:      true,
			expectCurrentNon: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := initRepoFor(t)
			// Initial commit under sub/.
			recordedSHA := commitFileTo(t, repo, "sub/a.go", "package sub\n", "initial")

			// Build the manifest as buildpipe would have written it.
			m := persist.Manifest{
				SrcRoot:         filepath.Join(repo, tc.srcSubdir),
				SrcRelPath:      tc.srcSubdir,
				SrcCommit:       recordedSHA,
				StalenessMethod: "git",
			}

			tc.repoMutation(t, repo)

			cur, stale := computeStaleness(m)
			if stale != tc.expectStale {
				t.Errorf("stale = %v, want %v", stale, tc.expectStale)
			}
			if tc.expectCurrentNon && cur == "" {
				t.Errorf("current = %q, want non-empty", cur)
			}
		})
	}
}

// TestComputeStaleness_LegacyBackcompat verifies that manifests written
// before the SrcRelPath field existed still resolve via the legacy
// `git rev-parse HEAD` path. This keeps existing /tmp/ckg-* graphs usable
// without forcing a rebuild.
func TestComputeStaleness_LegacyBackcompat(t *testing.T) {
	repo := initRepoFor(t)
	recordedSHA := commitFileTo(t, repo, "a.go", "package main\n", "initial")

	// Legacy manifest: SrcRoot is the repo itself (no sub-dir), SrcRelPath empty.
	m := persist.Manifest{
		SrcRoot:         repo,
		SrcCommit:       recordedSHA,
		StalenessMethod: "git",
		// SrcRelPath intentionally empty.
	}

	cur, stale := computeStaleness(m)
	if cur == "" {
		t.Errorf("current = empty, want HEAD SHA via legacy path")
	}
	if stale {
		t.Errorf("stale = true, want false (no further commits)")
	}

	// Now an unrelated commit lands. Under the legacy path this WILL flip
	// staleness — that is the documented limitation we kept for back-compat.
	commitFileTo(t, repo, "b.go", "package main\n// b\n", "second")
	_, stale2 := computeStaleness(m)
	if !stale2 {
		t.Errorf("stale = false after second commit; legacy path should detect HEAD movement")
	}
}

// TestComputeStaleness_LegacyBackcompat_DiscriminatesPathAware constructs a
// scenario where the legacy and path-aware branches disagree, so the test
// will fail loudly if a future refactor accidentally routes legacy manifests
// through the path-aware branch.
//
// Setup: SrcRoot points to a sub-directory inside a repo, SrcRelPath is
// empty (legacy). A new commit lands OUTSIDE that sub-directory.
//   - Legacy branch (`git -C srcRoot rev-parse HEAD`): HEAD moved → stale=true.
//     This is the documented limitation we deliberately retain for back-compat.
//   - Path-aware branch (`git log -1 -- <relPath>`): the unrelated commit
//     does NOT touch SrcRelPath → stale=false.
//
// If this assertion ever fails ("expected stale=true but got false"), someone
// has routed legacy manifests through the path-aware branch. That is a
// back-compat regression: every existing /tmp/ckg-* graph DB without
// SrcRelPath would silently stop showing the stale banner on HEAD movement.
func TestComputeStaleness_LegacyBackcompat_DiscriminatesPathAware(t *testing.T) {
	repo := initRepoFor(t)
	recordedSHA := commitFileTo(t, repo, "sub/a.go", "package sub\n", "initial in sub")

	// Legacy manifest pointing at a sub-directory. SrcRelPath intentionally
	// empty — exactly what an old manifest looks like.
	m := persist.Manifest{
		SrcRoot:         filepath.Join(repo, "sub"),
		SrcCommit:       recordedSHA,
		StalenessMethod: "git",
		// SrcRelPath intentionally empty (legacy).
	}

	// Commit OUTSIDE the sub-directory. Path-aware logic would consider this
	// irrelevant; legacy logic sees HEAD has moved and flips stale.
	commitFileTo(t, repo, "other/b.go", "package other\n", "outside sub")

	_, stale := computeStaleness(m)
	if !stale {
		t.Errorf("stale = false after outside commit; legacy branch must detect HEAD movement.\n" +
			"If this fails, someone routed legacy manifests through the path-aware branch — " +
			"that's a back-compat regression for existing /tmp/ckg-* graph DBs without SrcRelPath.")
	}
}

// TestComputeStaleness_NotGit verifies the function gracefully returns
// ("", false) when the recorded SrcRoot is not a git checkout.
func TestComputeStaleness_NotGit(t *testing.T) {
	srcRoot := t.TempDir()
	m := persist.Manifest{
		SrcRoot:         srcRoot,
		SrcRelPath:      ".",
		SrcCommit:       "deadbeef",
		StalenessMethod: "git",
	}
	cur, stale := computeStaleness(m)
	if cur != "" || stale {
		t.Errorf("got (%q, %v), want (\"\", false)", cur, stale)
	}
}

// TestComputeStaleness_NonGitMethod verifies the early return when the
// manifest was fingerprinted via mtime: no git invocation, no banner.
func TestComputeStaleness_NonGitMethod(t *testing.T) {
	m := persist.Manifest{
		SrcRoot:         "/anything",
		StalenessMethod: "mtime",
	}
	cur, stale := computeStaleness(m)
	if cur != "" || stale {
		t.Errorf("got (%q, %v), want (\"\", false)", cur, stale)
	}
}

// --- helpers (kept package-local; mirror buildpipe/staleness_test.go) ---

func initRepoFor(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	runGit(t, dir, "config", "user.email", "ckg-test@example.com")
	runGit(t, dir, "config", "user.name", "ckg-test")
	runGit(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

func commitFileTo(t *testing.T, repo, relPath, content, msg string) string {
	t.Helper()
	full := filepath.Join(repo, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit(t, repo, "add", relPath)
	runGit(t, repo, "commit", "-q", "-m", msg)
	out, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
