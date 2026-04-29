package temporal_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/temporal"
)

// TestLoadHistory_BasicThreeFiles initialises a tiny repo with three files
// across three commits, then verifies that LoadHistory populates Files +
// Commits with the expected (file → SHA list) shape.
//
// Sequence:
//
//	commit1: add a.txt
//	commit2: add b.txt
//	commit3: modify a.txt
//
// Expected Files:
//
//	a.txt → [commit3, commit1]   (most-recent-first)
//	b.txt → [commit2]
func TestLoadHistory_BasicThreeFiles(t *testing.T) {
	repo := initRepo(t)
	c1 := commitFile(t, repo, "a.txt", "alpha\n", "first")
	c2 := commitFile(t, repo, "b.txt", "bravo\n", "second")
	c3 := commitFile(t, repo, "a.txt", "alpha\nmore\n", "third (modify a)")

	hist, err := temporal.LoadHistory(repo, 10)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if got, want := len(hist.Files), 2; got != want {
		t.Errorf("Files count = %d, want %d (%v)", got, want, fileNames(hist.Files))
	}
	if got, want := len(hist.Commits), 3; got != want {
		t.Errorf("Commits count = %d, want %d", got, want)
	}
	wantA := []string{c3, c1}
	if !sliceEq(hist.Files["a.txt"], wantA) {
		t.Errorf("Files[a.txt] = %v, want %v", hist.Files["a.txt"], wantA)
	}
	wantB := []string{c2}
	if !sliceEq(hist.Files["b.txt"], wantB) {
		t.Errorf("Files[b.txt] = %v, want %v", hist.Files["b.txt"], wantB)
	}
	// Commit metadata sanity: subject + non-zero timestamp recorded.
	if hist.Commits[c1].Subject == "" || hist.Commits[c1].Timestamp == 0 {
		t.Errorf("Commit[c1] missing subject/timestamp: %+v", hist.Commits[c1])
	}
}

// TestLoadHistory_MostRecentFirst pins down the ordering invariant: the
// first SHA in Files[<path>] is the most recent commit touching that path.
func TestLoadHistory_MostRecentFirst(t *testing.T) {
	repo := initRepo(t)
	first := commitFile(t, repo, "x.txt", "a\n", "first")
	second := commitFile(t, repo, "x.txt", "ab\n", "second")
	third := commitFile(t, repo, "x.txt", "abc\n", "third")

	hist, err := temporal.LoadHistory(repo, 10)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	got := hist.Files["x.txt"]
	want := []string{third, second, first}
	if !sliceEq(got, want) {
		t.Errorf("Files[x.txt] order = %v, want %v", got, want)
	}
}

// TestLoadHistory_PerFileCap ensures maxPerFile bounds the per-file list.
func TestLoadHistory_PerFileCap(t *testing.T) {
	repo := initRepo(t)
	for i := 0; i < 5; i++ {
		commitFile(t, repo, "y.txt", strings.Repeat("y\n", i+1), "edit y")
	}
	hist, err := temporal.LoadHistory(repo, 3)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	got := hist.Files["y.txt"]
	if len(got) != 3 {
		t.Errorf("expected exactly 3 commits for y.txt under cap, got %d (%v)", len(got), got)
	}
}

// TestLoadHistory_NotAGitCheckout: when repoRoot has no .git, LoadHistory
// returns an empty FileHistory with nil error so the build pipeline can
// degrade gracefully (no temporal edges, no fatal).
func TestLoadHistory_NotAGitCheckout(t *testing.T) {
	dir := t.TempDir()
	hist, err := temporal.LoadHistory(dir, 10)
	if err != nil {
		t.Fatalf("LoadHistory: unexpected err on non-git dir: %v", err)
	}
	if len(hist.Files) != 0 || len(hist.Commits) != 0 {
		t.Errorf("expected empty history for non-git dir, got Files=%d Commits=%d",
			len(hist.Files), len(hist.Commits))
	}
}

// TestLoadHistory_PathWithSpaces verifies the TAB-based path parser
// preserves spaces (a naive whitespace split would corrupt the path).
func TestLoadHistory_PathWithSpaces(t *testing.T) {
	repo := initRepo(t)
	c1 := commitFile(t, repo, "a file.txt", "hello\n", "spaces")
	hist, err := temporal.LoadHistory(repo, 10)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	got := hist.Files["a file.txt"]
	if !sliceEq(got, []string{c1}) {
		t.Errorf("Files[\"a file.txt\"] = %v, want [%s]", got, c1)
	}
}

// --- shared test helpers ---

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init", "-q", "-b", "main")
	run(t, dir, "git", "config", "user.email", "ckg-test@example.com")
	run(t, dir, "git", "config", "user.name", "ckg-test")
	run(t, dir, "git", "config", "commit.gpgsign", "false")
	return dir
}

func commitFile(t *testing.T, repo, relPath, content, msg string) string {
	t.Helper()
	full := filepath.Join(repo, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run(t, repo, "git", "add", "--", relPath)
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

func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func fileNames(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
