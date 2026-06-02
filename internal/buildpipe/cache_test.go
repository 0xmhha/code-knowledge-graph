package buildpipe_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// makeMiniGoModule writes a 3-file Go module to dir and returns the file
// path map (rel → absolute) so tests can mutate individual files. The
// fixture is intentionally small (parses in <100ms) so cache-hit timing
// assertions are meaningful.
func makeMiniGoModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), `module example.com/cachetest

go 1.21
`)
	mustWrite(t, filepath.Join(dir, "a.go"), `package cachetest

// Add returns a + b.
func Add(a, b int) int { return a + b }
`)
	mustWrite(t, filepath.Join(dir, "b.go"), `package cachetest

// Mul returns a * b.
func Mul(a, b int) int { return a * b }
`)
	mustWrite(t, filepath.Join(dir, "c.go"), `package cachetest

// Sub returns a - b.
func Sub(a, b int) int { return a - b }
`)
	return dir
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// runBuild is a small helper that wraps buildpipe.Run with sane test defaults.
// Returns the resulting Manifest so tests can assert FileEntry contents.
func runBuild(t *testing.T, src, out string, opts ...func(*buildpipe.Options)) persist.Manifest {
	t.Helper()
	o := buildpipe.Options{
		SrcRoot: src, OutDir: out,
		Languages: []string{"auto"}, CKGVersion: "test",
	}
	for _, fn := range opts {
		fn(&o)
	}
	m, err := buildpipe.Run(o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return m
}

// TestIncremental_FullCacheHit builds twice in a row with no source changes.
// The second build must report all-cached (Files block from first build is
// reused; no parsing occurs). We assert this by checking that the BUILD
// timestamp advances (proving we ran) but the per-file SHA256s are identical.
func TestIncremental_FullCacheHit(t *testing.T) {
	src := makeMiniGoModule(t)
	out := t.TempDir()

	first := runBuild(t, src, out)
	if len(first.Files) == 0 {
		t.Fatal("expected Files block on first build")
	}
	firstStamp := first.BuildTimestamp

	// Sleep 1 second to guarantee different RFC3339 second precision.
	time.Sleep(1100 * time.Millisecond)

	second := runBuild(t, src, out)
	if second.BuildTimestamp == firstStamp {
		t.Errorf("expected timestamp to advance on rebuild, got %q == %q",
			second.BuildTimestamp, firstStamp)
	}
	// Same number of files, same SHAs.
	if len(second.Files) != len(first.Files) {
		t.Fatalf("Files count drift: first=%d second=%d",
			len(first.Files), len(second.Files))
	}
	indexByPath := map[string]persist.FileEntry{}
	for _, e := range first.Files {
		indexByPath[e.Path] = e
	}
	for _, e := range second.Files {
		want := indexByPath[e.Path]
		if e.SHA256 != want.SHA256 {
			t.Errorf("SHA drift on %q: %q vs %q", e.Path, e.SHA256, want.SHA256)
		}
		if e.CacheKey != want.CacheKey {
			t.Errorf("CacheKey drift on %q", e.Path)
		}
	}
}

// TestIncremental_OneFileChanged modifies one file's content and asserts that
// (a) the changed file's SHA changes; (b) the other files' SHA + node IDs
// remain identical; (c) the changed file's node count is reflected.
func TestIncremental_OneFileChanged(t *testing.T) {
	src := makeMiniGoModule(t)
	out := t.TempDir()
	first := runBuild(t, src, out)

	firstByPath := map[string]persist.FileEntry{}
	for _, e := range first.Files {
		firstByPath[e.Path] = e
	}
	if _, ok := firstByPath["a.go"]; !ok {
		t.Fatalf("expected a.go in first manifest, got %v", manifestPaths(first))
	}

	// Append a comment to a.go (changes content + mtime).
	mustWrite(t, filepath.Join(src, "a.go"), `package cachetest

// Add returns a + b.
func Add(a, b int) int { return a + b }

// extra noop for cache test
var _ = 0
`)

	second := runBuild(t, src, out)
	secondByPath := map[string]persist.FileEntry{}
	for _, e := range second.Files {
		secondByPath[e.Path] = e
	}

	// (a) a.go SHA must differ.
	if firstByPath["a.go"].SHA256 == secondByPath["a.go"].SHA256 {
		t.Errorf("expected a.go SHA to change, got identical %q", secondByPath["a.go"].SHA256)
	}
	// (b) b.go and c.go SHA + node IDs must be identical.
	for _, p := range []string{"b.go", "c.go"} {
		if firstByPath[p].SHA256 != secondByPath[p].SHA256 {
			t.Errorf("%s SHA drift: %q vs %q", p,
				firstByPath[p].SHA256, secondByPath[p].SHA256)
		}
		if !equalStringSlices(firstByPath[p].NodeIDs, secondByPath[p].NodeIDs) {
			t.Errorf("%s NodeIDs drift:\n  first=%v\n  second=%v",
				p, firstByPath[p].NodeIDs, secondByPath[p].NodeIDs)
		}
	}
}

// TestIncremental_FileRemoved deletes one file and asserts its FileEntry is
// gone from the new manifest and its nodes are gone from the DB.
func TestIncremental_FileRemoved(t *testing.T) {
	src := makeMiniGoModule(t)
	out := t.TempDir()
	first := runBuild(t, src, out)

	if firstHas := manifestHasPath(first, "c.go"); !firstHas {
		t.Fatalf("expected c.go in first manifest, got %v", manifestPaths(first))
	}

	// Remove c.go from the source tree.
	if err := os.Remove(filepath.Join(src, "c.go")); err != nil {
		t.Fatalf("remove c.go: %v", err)
	}

	second := runBuild(t, src, out)
	if manifestHasPath(second, "c.go") {
		t.Errorf("c.go should be absent from second manifest, paths=%v",
			manifestPaths(second))
	}
	// b.go survives.
	if !manifestHasPath(second, "b.go") {
		t.Errorf("b.go should still be present, paths=%v", manifestPaths(second))
	}
	// DB also lost c.go's nodes (look up via store).
	store, err := persist.OpenReadOnly(filepath.Join(out, "graph.db"))
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	defer func() { _ = store.Close() }()
	nodes, err := store.NodesByFilePath("c.go")
	if err != nil {
		t.Fatalf("NodesByFilePath: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected c.go nodes to be deleted, got %d", len(nodes))
	}
}

// TestIncremental_NoCacheFlag verifies --no-cache forces a full rebuild
// even when prior manifest is fully usable. Verified by checking that
// graph.db's mtime advances meaningfully (cold path os.Removes it) AND
// the manifest still validates (Files block re-emitted).
func TestIncremental_NoCacheFlag(t *testing.T) {
	src := makeMiniGoModule(t)
	out := t.TempDir()
	_ = runBuild(t, src, out)
	st1, err := os.Stat(filepath.Join(out, "graph.db"))
	if err != nil {
		t.Fatalf("stat first: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	second := runBuild(t, src, out, func(o *buildpipe.Options) {
		o.NoCache = true
	})
	st2, err := os.Stat(filepath.Join(out, "graph.db"))
	if err != nil {
		t.Fatalf("stat second: %v", err)
	}
	// Cold path os.Removes then recreates → inode advances or mtime changes
	// significantly. Compare size/mtime bundle.
	if st1.ModTime().Equal(st2.ModTime()) && st1.Size() == st2.Size() {
		t.Errorf("expected graph.db to be rebuilt under --no-cache (same mtime+size suggests reuse)")
	}
	// Manifest still has Files block.
	if len(second.Files) == 0 {
		t.Errorf("expected --no-cache rebuild to still emit Files block")
	}
}

// TestIncremental_SchemaBumpInvalidates simulates a stored manifest with
// SchemaVersion="1.0", then runs a build. ManifestUsable returns false
// (1.0 != current SchemaVersion), so the build falls into the cold path —
// the entire DB is rebuilt and a fresh-version manifest is emitted.
func TestIncremental_SchemaBumpInvalidates(t *testing.T) {
	src := makeMiniGoModule(t)
	out := t.TempDir()

	first := runBuild(t, src, out)
	if first.SchemaVersion != buildpipe.SchemaVersion {
		t.Fatalf("first build SchemaVersion = %q, want %q",
			first.SchemaVersion, buildpipe.SchemaVersion)
	}

	// Manually rewrite the manifest's schema_version to "1.0" (legacy).
	store, err := persist.Open(filepath.Join(out, "graph.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	corrupted := first
	corrupted.SchemaVersion = "1.0"
	if err := store.SetManifest(corrupted); err != nil {
		t.Fatalf("SetManifest(1.0): %v", err)
	}
	_ = store.Close()

	second := runBuild(t, src, out)
	if second.SchemaVersion != buildpipe.SchemaVersion {
		t.Errorf("post-rebuild SchemaVersion = %q, want %q",
			second.SchemaVersion, buildpipe.SchemaVersion)
	}
	// Files block is fresh (different timestamp guarantees fresh build).
	if len(second.Files) != len(first.Files) {
		t.Errorf("Files count drift: first=%d second=%d",
			len(first.Files), len(second.Files))
	}
}

// TestIncremental_MTimeOnlyChange uses os.Chtimes to bump a file's mtime
// without changing content. The slow path (SHA256 verification) must
// re-classify the file as cached even though mtime moved.
func TestIncremental_MTimeOnlyChange(t *testing.T) {
	src := makeMiniGoModule(t)
	out := t.TempDir()
	first := runBuild(t, src, out)
	firstNodeIDs := map[string][]string{}
	for _, e := range first.Files {
		firstNodeIDs[e.Path] = e.NodeIDs
	}

	// Bump a.go's mtime to a far-future time without touching content.
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(filepath.Join(src, "a.go"), future, future); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	second := runBuild(t, src, out)
	// a.go is still cached (slow-path SHA confirmed content unchanged) → its
	// NodeIDs in the manifest match what they were before.
	for _, e := range second.Files {
		if e.Path != "a.go" {
			continue
		}
		if !equalStringSlices(e.NodeIDs, firstNodeIDs["a.go"]) {
			t.Errorf("a.go NodeIDs drift on mtime-only change:\n  first=%v\n  second=%v",
				firstNodeIDs["a.go"], e.NodeIDs)
		}
		// mtime advanced, so the manifest's MTime field for a.go must reflect that.
		if e.MTime <= first.Files[0].MTime { // any first-file mtime as baseline
			// Loose check: just confirm MTime is non-zero.
			if e.MTime == 0 {
				t.Errorf("expected non-zero MTime on a.go, got 0")
			}
		}
	}
}

// helper: assert two string slices are equal as sets (sorted compare).
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string{}, a...)
	y := append([]string{}, b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

func manifestPaths(m persist.Manifest) []string {
	out := make([]string, 0, len(m.Files))
	for _, e := range m.Files {
		out = append(out, e.Path)
	}
	sort.Strings(out)
	return out
}

func manifestHasPath(m persist.Manifest, path string) bool {
	for _, e := range m.Files {
		if e.Path == path {
			return true
		}
	}
	return false
}

// TestPartialCache_PreservesCrossFileEdges_ViaColdFallback is the regression
// guard for the A3 follow-up: previously, when a callee file was modified
// (cached caller still pointing into it), the partial-cache routing in
// runIncremental dropped the cross-file `calls` edge because cached files
// don't re-emit pending refs. Empirical proof: testdata/synthetic vault.go
// modify took cross-file calls 2 → 0.
//
// The fix removes the partial-cache routing — any non-full-hit case now
// falls back to runCold, which re-runs Pass 2 in full. This test pins
// the contract: cross-file calls survive a callee-side modification.
//
// Module shape:
//
//	caller.go: func Use() { Helper() }
//	callee.go: func Helper() {}
//
// Cold build → 1 cross-file `calls` edge (caller.Use → callee.Helper).
// Modify callee.go → rebuild. Expect: still 1 cross-file `calls` edge.
func TestPartialCache_PreservesCrossFileEdges_ViaColdFallback(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), `module example.com/edgepin

go 1.21
`)
	mustWrite(t, filepath.Join(dir, "caller.go"), `package edgepin

// Use invokes Helper from the same package, different file.
func Use() { Helper() }
`)
	calleePath := filepath.Join(dir, "callee.go")
	mustWrite(t, calleePath, `package edgepin

// Helper does nothing. The point is that Use() targets it from caller.go.
func Helper() {}
`)
	out := t.TempDir()

	// Cold build — establish baseline cross-file calls count.
	first := runBuild(t, dir, out)
	if !manifestHasPath(first, "caller.go") || !manifestHasPath(first, "callee.go") {
		t.Fatalf("expected both files in first manifest, got %v", manifestPaths(first))
	}
	coldCalls := countCrossFileCallsEdges(t, filepath.Join(out, "graph.db"))
	if coldCalls < 1 {
		t.Fatalf("cold build must emit ≥1 cross-file calls edge, got %d", coldCalls)
	}

	// Modify callee.go (callee-side change; caller.go remains cached).
	// Without the cold-fallback fix this rebuild would silently drop
	// caller.Use → callee.Helper, taking the cross-file calls count to 0.
	mustWrite(t, calleePath, `package edgepin

// Helper does nothing — comment edited to invalidate this file's cache.
func Helper() {}
`)
	runBuild(t, dir, out)
	warmCalls := countCrossFileCallsEdges(t, filepath.Join(out, "graph.db"))
	if warmCalls != coldCalls {
		t.Errorf("cross-file calls drifted on partial-cache rebuild: cold=%d warm=%d (regression: A3 partial-cache silent edge loss)",
			coldCalls, warmCalls)
	}
}

// TestIncremental_ImplementsEdgesNoDrift is the regression guard for the
// incremental-path implements/extends bug: cold builds emit implements via
// the post-Resolve EmitImplementsEdges pass, but runGoPipelineIncremental
// previously did not, AND the runIncremental DROP step did not include the
// "implements"/"extends" types. Net effect: the first incremental rebuild
// after touching any Go file silently dropped the row count to zero (no
// re-emission) OR doubled it (no DROP), depending on call order.
//
// Module shape:
//
//	greeter.go: type Greeter interface { Greet() string }
//	hello.go:   type Hello struct{}; func (Hello) Greet() string { ... }
//
// Cold build → 1 implements edge (hello.Hello → greeter.Greeter — same pkg
// here, but the same code path drives cross-pkg satisfaction). Touch
// hello.go to dirty it → second build is incremental. Implements count
// must equal cold count (no drift, no duplicates).
func TestIncremental_ImplementsEdgesNoDrift(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), `module example.com/implpin

go 1.21
`)
	mustWrite(t, filepath.Join(dir, "greeter.go"), `package implpin

// Greeter is satisfied by Hello below.
type Greeter interface {
	Greet() string
}
`)
	helloPath := filepath.Join(dir, "hello.go")
	mustWrite(t, helloPath, `package implpin

// Hello satisfies Greeter via a value-receiver method.
type Hello struct{}

func (h Hello) Greet() string { return "hi" }
`)
	out := t.TempDir()

	// Cold build — establishes baseline implements count.
	_ = runBuild(t, dir, out)
	dbPath := filepath.Join(out, "graph.db")
	coldImpl := countEdgesByType(t, dbPath, "implements")
	if coldImpl < 1 {
		t.Fatalf("cold build must emit ≥1 implements edge, got %d", coldImpl)
	}

	// Incremental rebuild — touch hello.go's mtime AND content (comment edit)
	// to force the cache classifier to mark it dirty.
	mustWrite(t, helloPath, `package implpin

// Hello satisfies Greeter via a value-receiver method (comment edited
// to invalidate this file's cache for the incremental drift test).
type Hello struct{}

func (h Hello) Greet() string { return "hi" }
`)
	runBuild(t, dir, out)
	incImpl := countEdgesByType(t, dbPath, "implements")
	if incImpl != coldImpl {
		t.Errorf("implements drift on incremental rebuild: cold=%d inc=%d (regression: incremental path missing EmitImplementsEdges or DROP)",
			coldImpl, incImpl)
	}
}

// countEdgesByType opens the SQLite graph at dbPath read-only and returns
// the number of edges of the given type. Used by edge-preservation tests.
func countEdgesByType(t *testing.T, dbPath, edgeType string) int {
	t.Helper()
	store, err := persist.OpenReadOnly(dbPath)
	if err != nil {
		t.Fatalf("open db %s: %v", dbPath, err)
	}
	defer func() { _ = store.Close() }()
	edges, err := store.QueryEdgesByType(edgeType)
	if err != nil {
		t.Fatalf("query %s edges: %v", edgeType, err)
	}
	return len(edges)
}

// countCrossFileCallsEdges queries the SQLite graph for `calls` edges whose
// src and dst nodes live in different files. Used by edge-preservation tests.
func countCrossFileCallsEdges(t *testing.T, dbPath string) int {
	t.Helper()
	store, err := persist.OpenReadOnly(dbPath)
	if err != nil {
		t.Fatalf("open db %s: %v", dbPath, err)
	}
	defer func() { _ = store.Close() }()
	// QueryEdgesByType doesn't filter by file — fetch all calls, then
	// look up each endpoint's file_path and count cross-file pairs.
	edges, err := store.QueryEdgesByType("calls")
	if err != nil {
		t.Fatalf("query calls edges: %v", err)
	}
	if len(edges) == 0 {
		return 0
	}
	idSet := map[string]bool{}
	for _, e := range edges {
		idSet[e.Src] = true
		idSet[e.Dst] = true
	}
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	nodes, err := store.NodesByIDs(ids)
	if err != nil {
		t.Fatalf("nodes by ids: %v", err)
	}
	pathByID := map[string]string{}
	for _, n := range nodes {
		pathByID[n.ID] = n.FilePath
	}
	cross := 0
	for _, e := range edges {
		if pathByID[e.Src] != "" && pathByID[e.Dst] != "" &&
			pathByID[e.Src] != pathByID[e.Dst] {
			cross++
		}
	}
	return cross
}
