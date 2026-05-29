package temporal

import (
	"os/exec"
	"strings"
	"testing"
)

// TestLoadUnreachableHunks_NotAGitRepo asserts the same graceful-degrade
// contract LoadHunks observes: non-git directories return (nil, nil, nil)
// without invoking git.
func TestLoadUnreachableHunks_NotAGitRepo(t *testing.T) {
	dir := t.TempDir()
	commits, hunks, err := LoadUnreachableHunks(dir, 0)
	if err != nil {
		t.Fatalf("LoadUnreachableHunks on non-git: err = %v, want nil", err)
	}
	if commits != nil || hunks != nil {
		t.Errorf("non-git: expected (nil, nil), got commits=%d hunks=%d", len(commits), len(hunks))
	}
}

// TestLoadUnreachableHunks_NoUnreachable: a fresh repo with all commits
// reachable from HEAD has nothing to surface — the function returns
// (nil, nil, nil) without erroring.
func TestLoadUnreachableHunks_NoUnreachable(t *testing.T) {
	repo := initUnreachFixtureRepo(t)
	commitFile(t, repo, "a.go", "package a\n", "first")
	commitFile(t, repo, "a.go", "package a\nvar X = 1\n", "edit")
	commits, hunks, err := LoadUnreachableHunks(repo, 0)
	if err != nil {
		t.Fatalf("LoadUnreachableHunks: %v", err)
	}
	if len(commits) != 0 || len(hunks) != 0 {
		t.Errorf("expected 0 unreachable commits/hunks on linear-history repo, got %d / %d",
			len(commits), len(hunks))
	}
}

// TestLoadUnreachableHunks_ResetCreatesUnreachable: a hard reset that
// rewinds two commits leaves the abandoned SHAs in the reflog and (if
// not in any ref) in fsck-unreachable. The pass should pick them up
// and emit hunks for the now-orphaned content.
func TestLoadUnreachableHunks_ResetCreatesUnreachable(t *testing.T) {
	repo := initUnreachFixtureRepo(t)
	commitFile(t, repo, "a.go", "package a\n", "first")
	commitFile(t, repo, "a.go", "package a\nvar X = 1\n", "second")
	// "second" is now reachable; capture it before rewinding.
	rewoundSHA := headSHA(t, repo)
	if rewoundSHA == "" {
		t.Fatalf("could not capture HEAD SHA before reset")
	}
	// Rewind one commit so "second" becomes orphaned.
	runCmdInRepo(t, repo, "reset", "--hard", "HEAD~1")
	// gc the reflog occasionally fails to surface the orphan immediately
	// on some git versions; skip --prune so the reflog still references
	// rewoundSHA. fsck --no-reflogs --unreachable is the more reliable
	// detector — let's not run gc at all to keep both surfaces alive.

	commits, hunks, err := LoadUnreachableHunks(repo, 0)
	if err != nil {
		t.Fatalf("LoadUnreachableHunks: %v", err)
	}
	if len(commits) == 0 {
		t.Fatalf("expected ≥1 unreachable commit after reset, got 0 (reflog/fsck silent?)")
	}
	// The orphaned SHA must be among them.
	found := false
	for _, ci := range commits {
		if ci.SHA == rewoundSHA {
			found = true
			break
		}
	}
	if !found {
		shas := make([]string, 0, len(commits))
		for _, ci := range commits {
			shas = append(shas, ci.SHA[:8])
		}
		t.Errorf("orphaned SHA %s not found in unreachable set %v",
			rewoundSHA[:8], strings.Join(shas, ", "))
	}
	if len(hunks) == 0 {
		t.Errorf("expected ≥1 hunk for orphaned commit, got 0")
	}
}

// TestLoadUnreachableHunks_CapHonoured: maxCommits caps the result.
func TestLoadUnreachableHunks_CapHonoured(t *testing.T) {
	repo := initUnreachFixtureRepo(t)
	commitFile(t, repo, "a.go", "package a\n", "first")
	for i := 0; i < 4; i++ {
		commitFile(t, repo, "a.go", "package a\n//"+strings.Repeat("e", i+1)+"\n", "edit")
	}
	// Reset back to "first" → the four edits become orphaned.
	runCmdInRepo(t, repo, "reset", "--hard", "HEAD~4")

	commits, _, err := LoadUnreachableHunks(repo, 2)
	if err != nil {
		t.Fatalf("LoadUnreachableHunks: %v", err)
	}
	if len(commits) > 2 {
		t.Errorf("cap=2 honoured? got %d commits", len(commits))
	}
}

// --- helpers ---

func initUnreachFixtureRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "ckg-test@example.com"},
		{"config", "user.name", "ckg-test"},
		{"config", "commit.gpgsign", "false"},
	} {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func runCmdInRepo(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func headSHA(t *testing.T, repo string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}
