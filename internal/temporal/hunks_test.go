package temporal

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseHunkHeader_Variants asserts the @@ header parser handles every
// shape git emits in the wild: the canonical `-S,L +S,L` form, the
// length-omitted `-S +S` form (when length is 1), and the malformed cases
// the build pipeline must tolerate without aborting.
func TestParseHunkHeader_Variants(t *testing.T) {
	cases := []struct {
		name                   string
		line                   string
		oldS, oldL, newS, newL int
		ok                     bool
	}{
		{"canonical", "@@ -10,5 +12,7 @@ func Foo()", 10, 5, 12, 7, true},
		{"length omitted", "@@ -42 +42 @@", 42, 1, 42, 1, true},
		{"length omitted old only", "@@ -42 +42,3 @@", 42, 1, 42, 3, true},
		{"newly-added file", "@@ -0,0 +1,5 @@", 0, 0, 1, 5, true},
		{"deleted file", "@@ -1,3 +0,0 @@", 1, 3, 0, 0, true},
		{"missing trailing @@", "@@ -1,1 +1,1", 0, 0, 0, 0, false},
		{"non-numeric", "@@ -a,1 +1,1 @@", 0, 0, 0, 0, false},
		{"wrong prefix", "## -1 +1 @@", 0, 0, 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oldS, oldL, newS, newL, ok := parseHunkHeader(tc.line)
			if ok != tc.ok || oldS != tc.oldS || oldL != tc.oldL ||
				newS != tc.newS || newL != tc.newL {
				t.Errorf("parseHunkHeader(%q) = (%d,%d,%d,%d,%v), want (%d,%d,%d,%d,%v)",
					tc.line, oldS, oldL, newS, newL, ok,
					tc.oldS, tc.oldL, tc.newS, tc.newL, tc.ok)
			}
		})
	}
}

// TestParseDiffGitPath_Variants asserts the diff --git header parser uses
// the b-side (post-image) path. With --no-renames git emits a/<P> b/<P>
// with identical sides; for newly-added files the a-side is /dev/null
// in the --- header but the diff --git line still has matching paths.
func TestParseDiffGitPath_Variants(t *testing.T) {
	cases := map[string]string{
		"diff --git a/main.go b/main.go":                 "main.go",
		"diff --git a/sub/dir/file.ts b/sub/dir/file.ts": "sub/dir/file.ts",
		"diff --git a/with-dash b/with-dash":             "with-dash",
		"diff --git a/x b/y":                             "y", // post-rename name
		"diff --git \"a/with space\" \"b/with space\"":   "with space",
		"garbage no path":                                "",
	}
	for line, want := range cases {
		got := parseDiffGitPath(line)
		if got != want {
			t.Errorf("parseDiffGitPath(%q) = %q, want %q", line, got, want)
		}
	}
}

// TestParseHunkStream_StandardSingleHunk feeds a one-commit, one-file,
// one-hunk patch through the parser and verifies every HunkInfo field.
// This is the smallest end-to-end shape and locks down the "happy path".
func TestParseHunkStream_StandardSingleHunk(t *testing.T) {
	stream := strings.Join([]string{
		"COMMIT 1234567890abcdef1234567890abcdef12345678 1700000000 add hello",
		"diff --git a/hello.go b/hello.go",
		"new file mode 100644",
		"index 0000000..abcdef1",
		"--- /dev/null",
		"+++ b/hello.go",
		"@@ -0,0 +1,3 @@",
		"+package main",
		"+",
		"+func Hello() {}",
		"",
	}, "\n")
	out, err := parseHunkStream(bytes.NewBufferString(stream))
	if err != nil {
		t.Fatalf("parseHunkStream: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(out))
	}
	h := out[0]
	if h.SHA != "1234567890abcdef1234567890abcdef12345678" {
		t.Errorf("SHA = %q", h.SHA)
	}
	if h.FilePath != "hello.go" {
		t.Errorf("FilePath = %q, want hello.go", h.FilePath)
	}
	if h.Index != 0 {
		t.Errorf("Index = %d, want 0", h.Index)
	}
	if h.OldStart != 0 || h.OldLines != 0 || h.NewStart != 1 || h.NewLines != 3 {
		t.Errorf("range = (%d,%d → %d,%d), want (0,0 → 1,3)",
			h.OldStart, h.OldLines, h.NewStart, h.NewLines)
	}
	if h.Added != 3 || h.Removed != 0 {
		t.Errorf("Added/Removed = %d/%d, want 3/0", h.Added, h.Removed)
	}
	if !bytes.HasPrefix(h.Patch, []byte("@@ -0,0 +1,3 @@")) {
		t.Errorf("Patch missing @@ header: %q", string(h.Patch))
	}
	if !bytes.Contains(h.Patch, []byte("+package main")) {
		t.Errorf("Patch missing body: %q", string(h.Patch))
	}
}

// TestParseHunkStream_MultiHunkAndIndex commits one file with two distinct
// hunks; verifies per-(commit, file) Index is 0 then 1, and that flush
// between hunks doesn't bleed body lines across boundaries.
func TestParseHunkStream_MultiHunkAndIndex(t *testing.T) {
	stream := strings.Join([]string{
		"COMMIT aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 1700000100 split edits",
		"diff --git a/x.go b/x.go",
		"--- a/x.go",
		"+++ b/x.go",
		"@@ -1,3 +1,3 @@",
		" a",
		"-b",
		"+B",
		" c",
		"@@ -10,2 +10,2 @@",
		" d",
		"-e",
		"+E",
		"",
	}, "\n")
	out, err := parseHunkStream(bytes.NewBufferString(stream))
	if err != nil {
		t.Fatalf("parseHunkStream: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 hunks, got %d", len(out))
	}
	if out[0].Index != 0 || out[1].Index != 1 {
		t.Errorf("indices = %d, %d; want 0, 1", out[0].Index, out[1].Index)
	}
	if out[0].NewStart != 1 || out[1].NewStart != 10 {
		t.Errorf("NewStart = %d, %d; want 1, 10", out[0].NewStart, out[1].NewStart)
	}
	// First hunk body should not contain second hunk's lines.
	if bytes.Contains(out[0].Patch, []byte("d")) || bytes.Contains(out[0].Patch, []byte("E")) {
		t.Errorf("hunk[0] Patch leaked second hunk: %q", out[0].Patch)
	}
	if !bytes.Contains(out[1].Patch, []byte("+E")) {
		t.Errorf("hunk[1] Patch missing +E: %q", out[1].Patch)
	}
}

// TestParseHunkStream_BinaryDiff verifies a "Binary files differ" line
// emits one zero-content HunkInfo so downstream queries can still see
// "this commit touched x.bin" without blowing up the patch stream parse.
func TestParseHunkStream_BinaryDiff(t *testing.T) {
	stream := strings.Join([]string{
		"COMMIT bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb 1700000200 add image",
		"diff --git a/image.png b/image.png",
		"new file mode 100644",
		"Binary files /dev/null and b/image.png differ",
		"",
	}, "\n")
	out, err := parseHunkStream(bytes.NewBufferString(stream))
	if err != nil {
		t.Fatalf("parseHunkStream: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 binary hunk, got %d", len(out))
	}
	h := out[0]
	if !h.Binary {
		t.Errorf("Binary flag = false, want true")
	}
	if h.Added != 0 || h.Removed != 0 {
		t.Errorf("Added/Removed = %d/%d, want 0/0 for binary", h.Added, h.Removed)
	}
	if len(h.Patch) != 0 {
		t.Errorf("Patch should be empty for binary, got %d bytes", len(h.Patch))
	}
}

// TestParseHunkStream_ModeOnlyChange — a permission-only change emits
// `diff --git` + `old mode 100644 / new mode 100755` with no @@ block.
// The parser must NOT emit a hunk for it (no @@, no Binary line). The
// commit is reachable but contributes zero hunks.
func TestParseHunkStream_ModeOnlyChange(t *testing.T) {
	stream := strings.Join([]string{
		"COMMIT cccccccccccccccccccccccccccccccccccccccc 1700000300 chmod",
		"diff --git a/run.sh b/run.sh",
		"old mode 100644",
		"new mode 100755",
		"",
	}, "\n")
	out, err := parseHunkStream(bytes.NewBufferString(stream))
	if err != nil {
		t.Fatalf("parseHunkStream: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected 0 hunks for mode-only change, got %d", len(out))
	}
}

// TestParseHunkStream_MultipleCommits verifies the per-commit Index
// counter resets across commits, and that a flush at end-of-stream
// captures the very last hunk (no trailing-blank required).
func TestParseHunkStream_MultipleCommits(t *testing.T) {
	stream := strings.Join([]string{
		"COMMIT 1111111111111111111111111111111111111111 1700001000 first",
		"diff --git a/a.go b/a.go",
		"--- a/a.go",
		"+++ b/a.go",
		"@@ -1,1 +1,2 @@",
		" x",
		"+y",
		"COMMIT 2222222222222222222222222222222222222222 1700002000 second",
		"diff --git a/a.go b/a.go",
		"--- a/a.go",
		"+++ b/a.go",
		"@@ -2,1 +2,2 @@",
		" y",
		"+z",
	}, "\n")
	out, err := parseHunkStream(bytes.NewBufferString(stream))
	if err != nil {
		t.Fatalf("parseHunkStream: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 hunks across 2 commits, got %d", len(out))
	}
	if out[0].Index != 0 || out[1].Index != 0 {
		t.Errorf("expected per-commit Index=0 for both, got %d, %d",
			out[0].Index, out[1].Index)
	}
	if out[0].SHA == out[1].SHA {
		t.Errorf("SHAs must differ across commits, both = %s", out[0].SHA)
	}
}

// TestLoadHunks_NotAGitRepo asserts the graceful-degrade contract:
// non-git paths return (nil, nil) without invoking git.
func TestLoadHunks_NotAGitRepo(t *testing.T) {
	dir := t.TempDir()
	out, err := LoadHunks(dir, 0)
	if err != nil {
		t.Fatalf("LoadHunks on non-git: err = %v, want nil", err)
	}
	if out != nil {
		t.Errorf("LoadHunks on non-git: out = %v, want nil", out)
	}
}

// TestLoadHunks_RealRepo builds a tiny git repo (3 commits, 2 files,
// known patch shapes) and runs LoadHunks against it, asserting the
// integration end-to-end: git invocation succeeds, stream parses,
// hunks materialise with the expected counts and per-commit Index reset.
func TestLoadHunks_RealRepo(t *testing.T) {
	repo := initFixtureRepo(t)
	commitFile(t, repo, "a.go", "package a\n\nfunc Foo() {}\n", "add foo")
	commitFile(t, repo, "a.go", "package a\n\nfunc Foo() { return }\n", "edit foo body")
	commitFile(t, repo, "b.go", "package b\n", "add b")

	hunks, err := LoadHunks(repo, 0)
	if err != nil {
		t.Fatalf("LoadHunks: %v", err)
	}
	if len(hunks) < 3 {
		t.Fatalf("expected ≥3 hunks (one per commit, more on multi-line edits), got %d", len(hunks))
	}
	// All hunks must reference one of the two known files.
	for _, h := range hunks {
		if h.FilePath != "a.go" && h.FilePath != "b.go" {
			t.Errorf("unexpected FilePath %q", h.FilePath)
		}
		if len(h.SHA) != 40 {
			t.Errorf("SHA length = %d, want 40 (got %q)", len(h.SHA), h.SHA)
		}
	}
	// Per-commit Index must be 0-based contiguous within each (sha, file).
	idxCheck := map[string]int{}
	for _, h := range hunks {
		key := h.SHA + ":" + h.FilePath
		want := idxCheck[key]
		if h.Index != want {
			t.Errorf("non-contiguous Index for %s: got %d, want %d", key, h.Index, want)
		}
		idxCheck[key] = want + 1
	}
}

// TestLoadHunks_MaxCommitsCap verifies the maxCommits parameter actually
// caps the walk — committing 5 changes and asking for 2 should yield
// hunks from at most 2 distinct SHAs.
func TestLoadHunks_MaxCommitsCap(t *testing.T) {
	repo := initFixtureRepo(t)
	for i := 0; i < 5; i++ {
		commitFile(t, repo, "x.go", "package x\n//"+strings.Repeat("e ", i+1)+"\n", "edit")
	}
	hunks, err := LoadHunks(repo, 2)
	if err != nil {
		t.Fatalf("LoadHunks: %v", err)
	}
	shas := map[string]struct{}{}
	for _, h := range hunks {
		shas[h.SHA] = struct{}{}
	}
	if len(shas) > 2 {
		t.Errorf("expected ≤2 commits walked under cap=2, got %d distinct SHAs", len(shas))
	}
	if len(shas) == 0 {
		t.Errorf("expected ≥1 hunk under cap=2, got 0")
	}
}

// --- helpers (kept package-local; no exported testgit util yet — the
//      buildpipe tests have their own copy. Factor out when a third
//      callsite appears, per the existing YAGNI note in temporal_test).

func initFixtureRepo(t *testing.T) string {
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

func commitFile(t *testing.T, repo, rel, content, msg string) {
	t.Helper()
	full := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, args := range [][]string{
		{"add", "--", rel},
		{"commit", "-q", "-m", msg},
	} {
		c := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}
