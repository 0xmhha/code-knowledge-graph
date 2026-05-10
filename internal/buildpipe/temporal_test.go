package buildpipe

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestEmitTemporalEdges_BasicGitRepo wires the full E4 path: build a tiny
// git repo containing one Go file, hand-construct a Graph (mirroring what
// graph.Build would produce — a Package + File + Function node), then run
// emitTemporalEdges and verify:
//
//   - One NodeCommit appended (single commit in the repo).
//   - changed_in edges: one per (existing node, touched commit) pair.
//   - blame edge: from the File node → the only commit.
func TestEmitTemporalEdges_BasicGitRepo(t *testing.T) {
	repo := initGitRepo(t)
	relPath := "main.go"
	commitFileToRepo(t, repo, relPath, "package main\n\nfunc Hello() {}\n", "initial")

	g := buildSyntheticGraph(relPath)
	originalNodes := len(g.Nodes)
	originalEdges := len(g.Edges)

	if _, err := emitTemporalEdges(g, repo, discardLog(), 10); err != nil {
		t.Fatalf("emitTemporalEdges: %v", err)
	}

	// Exactly one commit node added.
	commits := nodesByType(g.Nodes, types.NodeCommit)
	if len(commits) != 1 {
		t.Fatalf("expected 1 Commit node, got %d", len(commits))
	}
	if !strings.HasPrefix(commits[0].QualifiedName, "commit:") {
		t.Errorf("Commit qname = %q, want prefix commit:", commits[0].QualifiedName)
	}
	if commits[0].SubKind != "git" {
		t.Errorf("Commit sub_kind = %q, want git", commits[0].SubKind)
	}
	if commits[0].Language != "git" {
		t.Errorf("Commit language = %q, want git (sentinel)", commits[0].Language)
	}

	// Edge bookkeeping: every original node in main.go should have a
	// changed_in edge to the commit; one blame edge from the File node;
	// schema 1.8 H1 also adds NodeHunk + has_hunk + (zero or more) adjacent
	// edges depending on how the file's content split into hunks.
	changedIn := edgesByType(g.Edges, types.EdgeChangedIn)
	if len(changedIn) != originalNodes {
		t.Errorf("expected %d changed_in edges (one per original node in main.go), got %d",
			originalNodes, len(changedIn))
	}
	blame := edgesByType(g.Edges, types.EdgeBlame)
	if len(blame) != 1 {
		t.Errorf("expected exactly 1 blame edge, got %d", len(blame))
	}
	hunks := nodesByType(g.Nodes, types.NodeHunk)
	hasHunk := edgesByType(g.Edges, types.EdgeHasHunk)
	adjacent := edgesByType(g.Edges, types.EdgeAdjacent)
	if len(hasHunk) != len(hunks) {
		t.Errorf("has_hunk edges (%d) must equal Hunk nodes (%d)", len(hasHunk), len(hunks))
	}
	// Single-file single-commit scenario: the file is wholly new, so the
	// content collapses to one hunk → 1 NodeHunk, 1 has_hunk, 0 adjacent.
	if len(hunks) != 1 {
		t.Errorf("expected 1 Hunk for newly-added file, got %d", len(hunks))
	}
	if len(adjacent) != 0 {
		t.Errorf("expected 0 adjacent edges (single hunk), got %d", len(adjacent))
	}
	// Sanity: total node + edge counts include all the new pieces.
	if want := originalNodes + 1 + len(hunks); len(g.Nodes) != want {
		t.Errorf("Node count = %d, want %d", len(g.Nodes), want)
	}
	modifies := edgesByType(g.Edges, types.EdgeModifies)
	if want := originalEdges + len(changedIn) + len(blame) + len(hasHunk) + len(adjacent) + len(modifies); len(g.Edges) != want {
		t.Errorf("Edge count = %d, want %d", len(g.Edges), want)
	}
	// In the BasicGitRepo fixture (Package + File + Function in main.go,
	// one whole-file hunk) we expect exactly 1 modifies edge: hunk →
	// Function. Package + File aren't in modifiesNodeWhitelist.
	if len(modifies) != 1 {
		t.Errorf("expected 1 modifies edge (hunk → Function), got %d", len(modifies))
	}

	// Validate post-emit so a regression in dangling refs surfaces here.
	if err := graph.Validate(g); err != nil {
		t.Errorf("graph.Validate after temporal: %v", err)
	}
}

// TestEmitTemporalEdges_NotAGitRepo verifies graceful degrade: the function
// returns nil and the graph is untouched.
func TestEmitTemporalEdges_NotAGitRepo(t *testing.T) {
	dir := t.TempDir() // not a git checkout
	g := buildSyntheticGraph("main.go")
	beforeNodes := len(g.Nodes)
	beforeEdges := len(g.Edges)
	if _, err := emitTemporalEdges(g, dir, discardLog(), 10); err != nil {
		t.Fatalf("expected nil err on non-git src, got %v", err)
	}
	if len(g.Nodes) != beforeNodes || len(g.Edges) != beforeEdges {
		t.Errorf("graph was mutated for non-git src: nodes %d→%d edges %d→%d",
			beforeNodes, len(g.Nodes), beforeEdges, len(g.Edges))
	}
}

// TestEmitTemporalEdges_MultipleCommitsAndBlameOrder commits twice to the
// same file then verifies:
//   - 2 commit nodes,
//   - changed_in edges = nodes_in_file × 2 commits,
//   - blame edge points at the MOST RECENT commit (latest SHA).
func TestEmitTemporalEdges_MultipleCommitsAndBlameOrder(t *testing.T) {
	repo := initGitRepo(t)
	relPath := "x.go"
	commitFileToRepo(t, repo, relPath, "package x\n", "first")
	c2 := commitFileToRepo(t, repo, relPath, "package x\n\n// edit\n", "second")

	g := buildSyntheticGraph(relPath)
	if _, err := emitTemporalEdges(g, repo, discardLog(), 10); err != nil {
		t.Fatalf("emitTemporalEdges: %v", err)
	}
	commits := nodesByType(g.Nodes, types.NodeCommit)
	if len(commits) != 2 {
		t.Fatalf("expected 2 Commit nodes, got %d", len(commits))
	}
	changedIn := edgesByType(g.Edges, types.EdgeChangedIn)
	want := 3 * 2 // 3 nodes in x.go (Package + File + Function) × 2 commits
	if len(changedIn) != want {
		t.Errorf("expected %d changed_in edges, got %d", want, len(changedIn))
	}
	blame := edgesByType(g.Edges, types.EdgeBlame)
	if len(blame) != 1 {
		t.Fatalf("expected 1 blame edge, got %d", len(blame))
	}
	// Look up the commit node behind the blame edge — it must be c2 (most
	// recent), not the earlier one. Map qname back to SHA.
	dstQname := nodeQname(g.Nodes, blame[0].Dst)
	wantQname := "commit:" + c2
	if dstQname != wantQname {
		t.Errorf("blame edge points at %q, want %q (most recent commit)", dstQname, wantQname)
	}
}

// TestEmitTemporalEdges_PerFileCap exercises the maxPerFile bound by
// committing 5 edits to the same file and asking for at most 2 commits per
// file. Expect 2 Commit nodes and 2 × nodes-in-file changed_in edges.
func TestEmitTemporalEdges_PerFileCap(t *testing.T) {
	repo := initGitRepo(t)
	rel := "y.go"
	for i := 0; i < 5; i++ {
		commitFileToRepo(t, repo, rel, "package y\n//"+strings.Repeat("edit ", i+1)+"\n", "edit")
	}
	g := buildSyntheticGraph(rel)
	if _, err := emitTemporalEdges(g, repo, discardLog(), 2); err != nil {
		t.Fatalf("emitTemporalEdges: %v", err)
	}
	commits := nodesByType(g.Nodes, types.NodeCommit)
	if len(commits) != 2 {
		t.Errorf("expected 2 Commit nodes under cap=2, got %d", len(commits))
	}
	if got, want := len(edgesByType(g.Edges, types.EdgeChangedIn)), 3*2; got != want {
		t.Errorf("expected %d changed_in edges (3 nodes × 2 commits), got %d", want, got)
	}
}

// --- helpers ---

// buildSyntheticGraph hand-constructs a 3-node Graph (Package + File +
// Function) all sharing the same FilePath. Mirrors what graph.Build would
// emit for a single Go file. Avoids spinning up the real parser inside this
// test — the temporal pass is parser-agnostic, it just walks Node.FilePath.
func buildSyntheticGraph(relPath string) *graph.Graph {
	pkgID := "pkg00000000aaaa11"
	fileID := "file0000aaaa2222"
	funcID := "func0000bbbb3333"
	return &graph.Graph{
		Nodes: []types.Node{
			{
				ID: pkgID, Type: types.NodePackage, Name: "main", QualifiedName: "main",
				FilePath: relPath, StartLine: 1, EndLine: 1, StartByte: 0, EndByte: 1,
				Language: "go", Confidence: types.ConfExtracted,
			},
			{
				ID: fileID, Type: types.NodeFile, Name: relPath, QualifiedName: "main/" + relPath,
				FilePath: relPath, StartLine: 1, EndLine: 1, StartByte: 0, EndByte: 1,
				Language: "go", Confidence: types.ConfExtracted,
			},
			{
				ID: funcID, Type: types.NodeFunction, Name: "Hello", QualifiedName: "main.Hello",
				FilePath: relPath, StartLine: 3, EndLine: 3, StartByte: 14, EndByte: 32,
				Language: "go", Confidence: types.ConfExtracted,
			},
		},
		Edges: []types.Edge{
			{Src: pkgID, Dst: fileID, Type: types.EdgeContains, FilePath: relPath, Count: 1, Confidence: types.ConfExtracted},
			{Src: fileID, Dst: funcID, Type: types.EdgeDefines, FilePath: relPath, Count: 1, Confidence: types.ConfExtracted},
		},
	}
}

func nodesByType(ns []types.Node, t types.NodeType) []types.Node {
	out := []types.Node{}
	for _, n := range ns {
		if n.Type == t {
			out = append(out, n)
		}
	}
	return out
}

func edgesByType(es []types.Edge, t types.EdgeType) []types.Edge {
	out := []types.Edge{}
	for _, e := range es {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

func nodeQname(ns []types.Node, id string) string {
	for _, n := range ns {
		if n.ID == id {
			return n.QualifiedName
		}
	}
	return ""
}

func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// initGitRepo / commitFileToRepo are renamed copies of staleness_test.go's
// helpers — kept here so this test file is self-contained (the package's
// test files live in two packages: external `_test` and internal).
// TODO: factor a shared `internal/buildpipe/internal/testgit` once a third
// callsite appears (YAGNI for now).
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runCmd(t, dir, "git", "init", "-q", "-b", "main")
	runCmd(t, dir, "git", "config", "user.email", "ckg-test@example.com")
	runCmd(t, dir, "git", "config", "user.name", "ckg-test")
	runCmd(t, dir, "git", "config", "commit.gpgsign", "false")
	return dir
}

func commitFileToRepo(t *testing.T, repo, relPath, content, msg string) string {
	t.Helper()
	full := filepath.Join(repo, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runCmd(t, repo, "git", "add", "--", relPath)
	runCmd(t, repo, "git", "commit", "-q", "-m", msg)
	out, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func runCmd(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
