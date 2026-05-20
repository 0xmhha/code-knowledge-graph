package buildpipe

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// TestSetStaleness_PathAwareGit verifies setStaleness records the SHA of the
// last commit that touched src_root — not the whole-repo HEAD. Each table
// entry initialises a temp git repo, makes commits according to a small
// scenario script, and asserts the recorded SrcCommit / SrcRelPath /
// StalenessMethod against the expected outcome.
func TestSetStaleness_PathAwareGit(t *testing.T) {
	type step struct {
		path    string // rel to repo root
		content string
		message string
	}
	cases := []struct {
		name               string
		steps              []step // in order
		srcSubdir          string // src_root relative to repo root
		expectMethod       string
		expectRelPath      string
		expectCommitOfStep int // 1-indexed step that the recorded SrcCommit must equal
	}{
		{
			name: "src_root is repo root, single commit",
			steps: []step{
				{"main.go", "package main\n", "initial"},
			},
			srcSubdir:          ".",
			expectMethod:       "git",
			expectRelPath:      ".",
			expectCommitOfStep: 1,
		},
		{
			name: "src_root is sub-dir; later commit elsewhere does NOT advance recorded SHA",
			steps: []step{
				{"sub/a.go", "package sub\n", "add sub"},   // step 1 (touches sub)
				{"other/b.go", "package other\n", "other"}, // step 2 (does NOT touch sub)
			},
			srcSubdir:          "sub",
			expectMethod:       "git",
			expectRelPath:      "sub",
			expectCommitOfStep: 1, // SHA must be the sub-touching commit, not HEAD
		},
		{
			name: "src_root is sub-dir; later commit inside sub DOES advance recorded SHA",
			steps: []step{
				{"sub/a.go", "package sub\n", "add sub"},                   // step 1
				{"other/b.go", "package other\n", "other"},                 // step 2
				{"sub/a.go", "package sub\n// edited\n", "edit sub again"}, // step 3
			},
			srcSubdir:          "sub",
			expectMethod:       "git",
			expectRelPath:      "sub",
			expectCommitOfStep: 3,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := initRepo(t)
			shas := make([]string, 0, len(tc.steps))
			for _, s := range tc.steps {
				shas = append(shas, commitFile(t, repo, s.path, s.content, s.message))
			}

			srcRoot := filepath.Join(repo, tc.srcSubdir)
			m := &persist.Manifest{SrcRoot: srcRoot}
			setStaleness(m, discardLogger())

			if m.StalenessMethod != tc.expectMethod {
				t.Fatalf("StalenessMethod = %q, want %q", m.StalenessMethod, tc.expectMethod)
			}
			if m.SrcRelPath != tc.expectRelPath {
				t.Errorf("SrcRelPath = %q, want %q", m.SrcRelPath, tc.expectRelPath)
			}
			wantSHA := shas[tc.expectCommitOfStep-1]
			if m.SrcCommit != wantSHA {
				t.Errorf("SrcCommit = %q, want %q (step %d)", m.SrcCommit, wantSHA, tc.expectCommitOfStep)
			}
		})
	}
}

// TestSetStaleness_NotInGit verifies setStaleness falls back to the mtime
// path when src_root is not inside a git checkout. The fallback must not
// crash and must record method="mtime" plus a non-nil StalenessFiles slice
// (possibly empty if detect.Walk found no source files).
func TestSetStaleness_NotInGit(t *testing.T) {
	srcRoot := t.TempDir()
	// Drop a Go file so detect.Walk has at least one entry to mtime-fingerprint.
	if err := os.WriteFile(filepath.Join(srcRoot, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	m := &persist.Manifest{SrcRoot: srcRoot}
	setStaleness(m, discardLogger())

	if m.StalenessMethod != "mtime" {
		t.Errorf("StalenessMethod = %q, want %q", m.StalenessMethod, "mtime")
	}
	if m.SrcCommit != "" {
		t.Errorf("SrcCommit = %q, want empty", m.SrcCommit)
	}
	if m.SrcRelPath != "" {
		t.Errorf("SrcRelPath = %q, want empty", m.SrcRelPath)
	}
	if m.StalenessMTimeSum == 0 {
		t.Errorf("StalenessMTimeSum = 0, want non-zero (file written above has mtime)")
	}
}

// TestSetStaleness_DetachedHead verifies the path-aware lookup still records
// the correct per-path SHA when the working tree is in detached-HEAD state.
// `git log -1 -- path` walks history from HEAD and is unaffected by the
// branch reference, but we exercise it explicitly to lock the behaviour.
func TestSetStaleness_DetachedHead(t *testing.T) {
	repo := initRepo(t)
	sha1 := commitFile(t, repo, "sub/a.go", "package sub\n", "first")
	commitFile(t, repo, "other/b.go", "package other\n", "second")

	// Detach HEAD onto sha1.
	run(t, repo, "git", "checkout", "--detach", sha1)

	srcRoot := filepath.Join(repo, "sub")
	m := &persist.Manifest{SrcRoot: srcRoot}
	setStaleness(m, discardLogger())

	if m.StalenessMethod != "git" {
		t.Fatalf("StalenessMethod = %q, want git", m.StalenessMethod)
	}
	if m.SrcCommit != sha1 {
		t.Errorf("SrcCommit = %q, want %q", m.SrcCommit, sha1)
	}
}

// --- test helpers ---

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// initRepo creates a fresh git repo in a t.TempDir() and configures the
// minimum identity needed for `git commit` to succeed in CI environments.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init", "-q", "-b", "main")
	run(t, dir, "git", "config", "user.email", "ckg-test@example.com")
	run(t, dir, "git", "config", "user.name", "ckg-test")
	run(t, dir, "git", "config", "commit.gpgsign", "false")
	return dir
}

// commitFile writes content to repo/relPath, stages it, commits with the
// given message, and returns the resulting commit SHA.
func commitFile(t *testing.T, repo, relPath, content, msg string) string {
	t.Helper()
	full := filepath.Join(repo, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run(t, repo, "git", "add", relPath)
	run(t, repo, "git", "commit", "-q", "-m", msg)
	out, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
