package buildpipe

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// initPRRepo materialises a temp directory with two commits whose
// subjects carry "(#NNN)" markers, so ScanPRHistory has something to
// chew on. Returns the temp root.
//
// Commit layout:
//
//	c1: edits foo.go lines 1–5,  subject "Add Foo (#42)"
//	c2: edits foo.go lines 8–12, subject "Fix Foo edge case (#43)"
//
// The first commit is treated as the root (empty parent), the second
// chains off it.
func initPRRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
			"GIT_COMMITTER_DATE=2026-05-01T12:00:00Z",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "-q", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	run("remote", "add", "origin", "https://github.com/test-owner/test-repo.git")

	writeFile(t, root, "foo.go", `package foo

func Foo() {
	println("first version")
}
`)
	run("add", "foo.go")
	cmd := exec.Command("git", "-C", root, "commit", "-q", "-m", "Add Foo (#42)")
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_AUTHOR_DATE=2026-05-01T12:00:00Z",
		"GIT_COMMITTER_DATE=2026-05-01T12:00:00Z",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit c1: %v\n%s", err, out)
	}

	// c2 extends foo.go so the patch range for c2 is lines 6+ (new lines
	// appended). Pre-c2 file was 6 lines incl. the trailing newline; c2
	// adds 5 more so the new-side hunk is ~lines 7–11.
	writeFile(t, root, "foo.go", `package foo

func Foo() {
	println("first version")
}

func Bar() {
	println("edge case")
	println("more")
}
`)
	run("add", "foo.go")
	cmd = exec.Command("git", "-C", root, "commit", "-q", "-m",
		"Fix Foo edge case (#43)\n\nThis is the body summary line.\nFurther detail.")
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_AUTHOR_DATE=2026-05-02T12:00:00Z",
		"GIT_COMMITTER_DATE=2026-05-02T12:00:00Z",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit c2: %v\n%s", err, out)
	}
	return root
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// TestScanPRHistory_BasicOverlap exercises the load-bearing case: a
// node whose [StartLine, EndLine] range intersects a PR-tagged
// commit's patch range gets the PRRef attached. The two-commit
// fixture above produces #42 (lines 1–5) and #43 (lines 7+); a node
// at lines 3–4 must see #42 alone, a node at lines 8–9 must see #43,
// and a span node 1–10 must see both.
func TestScanPRHistory_BasicOverlap(t *testing.T) {
	root := initPRRepo(t)

	nodes := []types.Node{
		{ID: "n1", FilePath: "foo.go", StartLine: 3, EndLine: 4,
			Type: types.NodeFunction, Name: "Foo", QualifiedName: "foo.Foo",
			Language: "go"},
		{ID: "n2", FilePath: "foo.go", StartLine: 8, EndLine: 9,
			Type: types.NodeFunction, Name: "Bar", QualifiedName: "foo.Bar",
			Language: "go"},
		{ID: "n3", FilePath: "foo.go", StartLine: 1, EndLine: 11,
			Type: types.NodeFile, Name: "foo.go", QualifiedName: "file:foo.go",
			Language: "go"},
	}

	got, err := ScanPRHistory(root, nodes)
	if err != nil {
		t.Fatalf("ScanPRHistory: %v", err)
	}

	if len(got["n1"]) != 1 || got["n1"][0].Number != 42 {
		t.Errorf("n1: want [#42], got %+v", got["n1"])
	}
	if len(got["n2"]) != 1 || got["n2"][0].Number != 43 {
		t.Errorf("n2: want [#43], got %+v", got["n2"])
	}
	if len(got["n3"]) != 2 {
		t.Errorf("n3 (file-span node): want 2 PRs, got %d (%+v)", len(got["n3"]), got["n3"])
	}
	// PR title strip should drop the (#43) suffix and keep the rest.
	if got["n2"][0].Title != "Fix Foo edge case" {
		t.Errorf("title strip: got %q, want \"Fix Foo edge case\"", got["n2"][0].Title)
	}
	// Body excerpt: P0 "왜" history captures the full cleaned body,
	// not just the first line. The fixture body is two lines; both
	// must survive trailer stripping.
	wantSummary := "This is the body summary line.\nFurther detail."
	if got["n2"][0].Summary != wantSummary {
		t.Errorf("summary body excerpt:\n  got  %q\n  want %q",
			got["n2"][0].Summary, wantSummary)
	}
	// Repo derivation from origin URL.
	if got["n2"][0].Repo != "test-owner/test-repo" {
		t.Errorf("repo: got %q, want \"test-owner/test-repo\"", got["n2"][0].Repo)
	}
	// Order on n3 is recency-descending so #43 comes before #42.
	if got["n3"][0].Number != 43 || got["n3"][1].Number != 42 {
		t.Errorf("n3 ordering: want [#43, #42], got [%d, %d]",
			got["n3"][0].Number, got["n3"][1].Number)
	}
}

// TestScanPRHistory_NonGitTree confirms a tree without `.git` returns
// an empty map and no error — PR breadcrumb is strictly additive
// metadata.
func TestScanPRHistory_NonGitTree(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "foo.go", "package foo\n")
	got, err := ScanPRHistory(root, []types.Node{
		{ID: "n1", FilePath: "foo.go", StartLine: 1, EndLine: 1,
			Type: types.NodeFile, Name: "foo.go", QualifiedName: "file:foo.go",
			Language: "go"},
	})
	if err != nil {
		t.Errorf("expected nil error for non-git tree, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map for non-git tree, got %+v", got)
	}
}

// TestScanPRHistory_EmptyInputs documents the early-return contract:
// empty srcRoot or nodes slice returns an empty map without spawning
// git at all (cheap fast path).
func TestScanPRHistory_EmptyInputs(t *testing.T) {
	got, err := ScanPRHistory("", []types.Node{{ID: "n1"}})
	if err != nil || len(got) != 0 {
		t.Errorf("empty srcRoot: got %+v, err=%v", got, err)
	}
	got, err = ScanPRHistory("/tmp", nil)
	if err != nil || len(got) != 0 {
		t.Errorf("nil nodes: got %+v, err=%v", got, err)
	}
}

// TestRangesOverlap is a smoke around the inclusive-both-ends overlap
// helper that drives the hunk × node match.
func TestRangesOverlap(t *testing.T) {
	cases := []struct {
		a1, a2, b1, b2 int
		want           bool
	}{
		{1, 5, 3, 8, true},   // partial overlap
		{1, 5, 6, 10, false}, // no overlap, b above a
		{6, 10, 1, 5, false}, // no overlap, a above b
		{1, 10, 5, 6, true},  // a contains b
		{5, 6, 1, 10, true},  // b contains a
		{5, 5, 5, 5, true},   // single-line both
		{5, 5, 6, 6, false},  // adjacent single lines
	}
	for _, c := range cases {
		if got := rangesOverlap(c.a1, c.a2, c.b1, c.b2); got != c.want {
			t.Errorf("rangesOverlap(%d,%d,%d,%d) = %v, want %v",
				c.a1, c.a2, c.b1, c.b2, got, c.want)
		}
	}
}

// _ "discards" the time.Time temporal field — kept in scope so the
// (#NNN) regex test below can lean on time.RFC3339 parsing without an
// unused-import warning.
var _ = time.RFC3339

// TestBodyExcerpt locks down the P0 "왜" history transformation —
// the cleaned body that drives CKV's semantic search. The function
// must (a) keep the rationale prose intact, (b) drop the canonical
// set of git trailers, (c) cap runaway bodies on a line boundary
// with an ellipsis marker. See docs/PROJECT-BLUEPRINT-ALIGNMENT.md
// §4.2 P0 #1.
func TestBodyExcerpt(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "empty",
			body: "",
			want: "",
		},
		{
			name: "single line",
			body: "Just one explanation.",
			want: "Just one explanation.",
		},
		{
			name: "multi-paragraph body",
			body: "First paragraph explains why.\n" +
				"Continued reasoning.\n\n" +
				"Second paragraph: trade-off considered.",
			want: "First paragraph explains why.\n" +
				"Continued reasoning.\n\n" +
				"Second paragraph: trade-off considered.",
		},
		{
			name: "trailers dropped",
			body: "Fix race in commit pipeline.\n\n" +
				"Signed-off-by: Alice <alice@example.com>\n" +
				"Co-authored-by: Bob <bob@example.com>\n" +
				"Reviewed-by: Carol <carol@example.com>",
			want: "Fix race in commit pipeline.",
		},
		{
			name: "Generated with attribution dropped",
			body: "Real reason for the change.\n\n" +
				"Generated with [Claude Code](https://example.com)",
			want: "Real reason for the change.",
		},
		{
			name: "body that is entirely trailers",
			body: "Signed-off-by: Alice <alice@example.com>\n" +
				"Co-authored-by: Bob <bob@example.com>",
			want: "",
		},
		{
			name: "prose 'Reviewed: foo' kept (not a canonical trailer)",
			body: "Reviewed the failure modes and picked option B.",
			want: "Reviewed the failure modes and picked option B.",
		},
		{
			name: "trailing whitespace and CR stripped per-line",
			body: "Line one.   \r\nLine two.\t\r\n",
			want: "Line one.\nLine two.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bodyExcerpt(tc.body); got != tc.want {
				t.Errorf("bodyExcerpt mismatch:\n  got  %q\n  want %q", got, tc.want)
			}
		})
	}
}

// TestBodyExcerpt_LineBoundaryCap ensures a body that exceeds
// bodyExcerptMaxBytes is truncated at the previous newline and gets
// the "…" clipping marker so consumers know the excerpt is partial.
func TestBodyExcerpt_LineBoundaryCap(t *testing.T) {
	// Build a body well over the cap. Each line is ~80 bytes; 64 of
	// them is ~5 KB, comfortably above the 2 KB cap.
	const lineCount = 64
	lines := make([]string, lineCount)
	for i := range lines {
		lines[i] = "Paragraph line that explains a specific aspect of the change."
	}
	body := strings.Join(lines, "\n")
	got := bodyExcerpt(body)
	if len(got) > bodyExcerptMaxBytes+8 {
		t.Errorf("excerpt exceeded cap: len=%d (cap %d + ellipsis slack)", len(got), bodyExcerptMaxBytes)
	}
	if !strings.HasSuffix(got, "\n…") {
		t.Errorf("excerpt missing trailing ellipsis marker; last 20 chars = %q",
			got[max(0, len(got)-20):])
	}
	if strings.HasSuffix(strings.TrimSuffix(got, "\n…"), " ") {
		t.Errorf("truncation didn't land on a line boundary; tail = %q",
			got[max(0, len(got)-40):])
	}
}
