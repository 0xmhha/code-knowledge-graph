package persist_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"sort"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// newFixtureStore creates an in-process SQLite store populated with:
//
//	4 nodes : pkg (Package), funcA (Function), funcB (Function), funcC (Function in pkg2)
//	3 edges : contains(pkg→funcA), calls(funcA→funcB), calls(funcB→funcC)
//	1 blob  : attached to funcA
//	FTS     : rebuilt after inserts so SearchFTS works
//
// Returns persist.Store (interface). The concrete *sqliteStore is unexported,
// so test code interacts with it exclusively through the public interfaces.
func newFixtureStore(t *testing.T) persist.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "fixture.db")
	s, err := persist.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	nodes := []types.Node{
		{
			ID:            "pkg_000000000000", // 16 chars
			Type:          types.NodePackage,
			Name:          "mypkg",
			QualifiedName: "mypkg",
			FilePath:      "mypkg/mypkg.go",
			StartLine:     1, EndLine: 100, StartByte: 0, EndByte: 500,
			Language:   "go",
			Confidence: types.ConfExtracted,
		},
		{
			ID:            "funcA00000000000", // 16 chars
			Type:          types.NodeFunction,
			Name:          "FuncA",
			QualifiedName: "mypkg.FuncA",
			FilePath:      "mypkg/a.go",
			StartLine:     10, EndLine: 20, StartByte: 50, EndByte: 200,
			Language:   "go",
			Confidence: types.ConfExtracted,
			Signature:  "func FuncA()",
			DocComment: "FuncA does something useful",
		},
		{
			ID:            "funcB00000000000", // 16 chars
			Type:          types.NodeFunction,
			Name:          "FuncB",
			QualifiedName: "mypkg.FuncB",
			FilePath:      "mypkg/b.go",
			StartLine:     30, EndLine: 40, StartByte: 300, EndByte: 400,
			Language:   "go",
			Confidence: types.ConfInferred,
		},
		{
			ID:            "funcC00000000000", // 16 chars
			Type:          types.NodeFunction,
			Name:          "FuncC",
			QualifiedName: "pkg2.FuncC",
			FilePath:      "pkg2/c.go",
			StartLine:     1, EndLine: 10, StartByte: 0, EndByte: 100,
			Language:   "go",
			Confidence: types.ConfAmbiguous,
		},
	}
	if err := s.InsertNodes(nodes); err != nil {
		t.Fatalf("InsertNodes: %v", err)
	}

	edges := []types.Edge{
		{
			Src: "pkg_000000000000", Dst: "funcA00000000000",
			Type: types.EdgeContains, Count: 1, Confidence: types.ConfExtracted,
		},
		{
			Src: "funcA00000000000", Dst: "funcB00000000000",
			Type: types.EdgeCalls, Count: 3, Confidence: types.ConfExtracted,
		},
		{
			Src: "funcB00000000000", Dst: "funcC00000000000",
			Type: types.EdgeCalls, Count: 1, Confidence: types.ConfInferred,
		},
	}
	if err := s.InsertEdges(edges); err != nil {
		t.Fatalf("InsertEdges: %v", err)
	}

	// FTS5 is a content table (content='nodes') — no auto-trigger populates it.
	// RebuildFTS() issues INSERT INTO nodes_fts(nodes_fts) VALUES('rebuild').
	if err := s.RebuildFTS(); err != nil {
		t.Fatalf("RebuildFTS: %v", err)
	}

	// Blob for funcA only.
	blobs := map[string][]byte{
		"funcA00000000000": []byte("func FuncA() { /* source */ }"),
	}
	if err := s.InsertBlobs(blobs); err != nil {
		t.Fatalf("InsertBlobs: %v", err)
	}

	return s
}

// nodeIDs extracts IDs from a node slice for easy assertions.
func nodeIDs(ns []types.Node) []string {
	ids := make([]string, len(ns))
	for i, n := range ns {
		ids[i] = n.ID
	}
	sort.Strings(ids)
	return ids
}

// containsID reports whether id is in ids.
func containsID(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// TestPendingRefs (G6 v3, schema 1.5)
// ---------------------------------------------------------------------------

// TestPendingRefs_InsertAndReload covers the cold→partial round-trip: insert
// per-file unresolved refs from cold path's Pass 2 stage, reload them by
// file_path on the next partial-cache rebuild.
func TestPendingRefs_InsertAndReload(t *testing.T) {
	s := newFixtureStore(t)
	rows := []persist.PendingRefRow{
		{FilePath: "mypkg/a.go", SrcID: "funcA00000000000",
			TargetQName: "pkg2.FuncC", EdgeType: "calls", Line: 12, HintFile: "pkg2/c.go"},
		{FilePath: "mypkg/a.go", SrcID: "funcA00000000000",
			TargetQName: "pkg2.OtherFn", EdgeType: "calls", Line: 18},
		{FilePath: "mypkg/b.go", SrcID: "funcB00000000000",
			TargetQName: "pkg2.FuncC", EdgeType: "calls", Line: 35},
	}
	if err := s.InsertPendingRefs(rows); err != nil {
		t.Fatalf("InsertPendingRefs: %v", err)
	}
	got, err := s.PendingRefsByFilePath("mypkg/a.go")
	if err != nil {
		t.Fatalf("PendingRefsByFilePath: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d refs for a.go, want 2 (have %v)", len(got), got)
	}
	for _, r := range got {
		if r.SrcID != "funcA00000000000" {
			t.Errorf("unexpected SrcID %q", r.SrcID)
		}
	}
	// b.go should yield exactly one
	gotB, err := s.PendingRefsByFilePath("mypkg/b.go")
	if err != nil || len(gotB) != 1 {
		t.Errorf("b.go: got %v, want 1 ref (err=%v)", gotB, err)
	}
}

// TestPendingRefs_CascadeOnNodeDelete verifies that DeleteNodesByFilePath
// removes pending_refs whose src_id is in the deleted file's nodes, via
// FK ON DELETE CASCADE. Critical for partial-cache: when a dirty file is
// re-parsed, its old pending refs must vanish before fresh ones are inserted.
func TestPendingRefs_CascadeOnNodeDelete(t *testing.T) {
	s := newFixtureStore(t)
	rows := []persist.PendingRefRow{
		{FilePath: "mypkg/a.go", SrcID: "funcA00000000000",
			TargetQName: "pkg2.X", EdgeType: "calls", Line: 5},
		{FilePath: "mypkg/b.go", SrcID: "funcB00000000000",
			TargetQName: "pkg2.Y", EdgeType: "calls", Line: 9},
	}
	if err := s.InsertPendingRefs(rows); err != nil {
		t.Fatalf("InsertPendingRefs: %v", err)
	}
	// Drop the a.go file's nodes — funcA's pending refs must cascade out.
	if err := s.DeleteNodesByFilePath("mypkg/a.go"); err != nil {
		t.Fatalf("DeleteNodesByFilePath: %v", err)
	}
	gotA, _ := s.PendingRefsByFilePath("mypkg/a.go")
	if len(gotA) != 0 {
		t.Errorf("CASCADE failed: a.go still has %d pending refs", len(gotA))
	}
	gotB, _ := s.PendingRefsByFilePath("mypkg/b.go")
	if len(gotB) != 1 {
		t.Errorf("collateral damage: b.go pending refs = %d, want 1", len(gotB))
	}
}

// TestPendingRefs_PrimaryKeyDeduplicates — INSERT OR IGNORE drops duplicate
// (file_path, src_id, target_qname, edge_type, line) rows so the cold path's
// natural insertion of the same logical ref twice doesn't error out.
func TestPendingRefs_PrimaryKeyDeduplicates(t *testing.T) {
	s := newFixtureStore(t)
	row := persist.PendingRefRow{
		FilePath: "mypkg/a.go", SrcID: "funcA00000000000",
		TargetQName: "pkg2.X", EdgeType: "calls", Line: 7,
	}
	if err := s.InsertPendingRefs([]persist.PendingRefRow{row, row}); err != nil {
		t.Fatalf("InsertPendingRefs (dup): %v", err)
	}
	got, _ := s.PendingRefsByFilePath("mypkg/a.go")
	if len(got) != 1 {
		t.Errorf("PK dedup failed: got %d, want 1", len(got))
	}
}

// ---------------------------------------------------------------------------
// TestMigrate_DispatchKindIdempotent (Track C P1b, schema 1.7)
// ---------------------------------------------------------------------------

// TestMigrate_DispatchKindIdempotent verifies that calling Migrate() against
// a freshly-created DB (which already has dispatch_kind via the schema.sql
// CREATE TABLE) is a no-op — the ALTER ADD COLUMN inside ensureDispatchKindColumn
// must detect the column and return without error.
//
// Also verifies that dispatch_kind round-trips: writing an `invokes` edge
// with a non-empty dispatch_kind and reading it back via QueryEdgesByType
// preserves the value.
func TestMigrate_DispatchKindIdempotent(t *testing.T) {
	s := newFixtureStore(t)
	// Re-run Migrate — must be idempotent. ensureDispatchKindColumn detects
	// the existing column via PRAGMA table_info and returns without error.
	type migrator interface{ Migrate() error }
	m, ok := s.(migrator)
	if !ok {
		t.Fatalf("store does not implement Migrate()")
	}
	if err := m.Migrate(); err != nil {
		t.Fatalf("re-Migrate (idempotency): %v", err)
	}
	// Insert one invokes edge with each dispatch_kind value, plus a static
	// `calls` edge with empty dispatch_kind.
	edges := []types.Edge{
		{Src: "funcA00000000000", Dst: "funcB00000000000",
			Type: types.EdgeCalls, Count: 1, Confidence: types.ConfExtracted},
		{Src: "funcA00000000000", Dst: "funcB00000000000",
			Type: types.EdgeInvokes, Count: 1, Confidence: types.ConfExtracted,
			DispatchKind: "interface_method"},
		{Src: "funcA00000000000", Dst: "funcA00000000000",
			Type: types.EdgeInvokes, Count: 1, Confidence: types.ConfExtracted,
			DispatchKind: "closure"},
	}
	if err := s.InsertEdges(edges); err != nil {
		t.Fatalf("InsertEdges: %v", err)
	}
	got, err := s.QueryEdgesByType(string(types.EdgeInvokes))
	if err != nil {
		t.Fatalf("QueryEdgesByType: %v", err)
	}
	byKind := map[string]int{}
	for _, e := range got {
		byKind[e.DispatchKind]++
	}
	if byKind["interface_method"] != 1 {
		t.Errorf("interface_method invokes: got %d, want 1", byKind["interface_method"])
	}
	if byKind["closure"] != 1 {
		t.Errorf("closure invokes: got %d, want 1", byKind["closure"])
	}
	// Static `calls` edges must have empty dispatch_kind on read-back.
	calls, _ := s.QueryEdgesByType(string(types.EdgeCalls))
	for _, e := range calls {
		if e.DispatchKind != "" {
			t.Errorf("calls edge has dispatch_kind=%q, want empty", e.DispatchKind)
		}
	}
}

// ---------------------------------------------------------------------------
// TestDistinctFilePaths
// ---------------------------------------------------------------------------

func TestDistinctFilePaths_Go(t *testing.T) {
	s := newFixtureStore(t)

	paths, err := s.DistinctFilePaths("go")
	if err != nil {
		t.Fatalf("DistinctFilePaths: %v", err)
	}
	// Fixture has 4 nodes across 4 distinct file paths
	// (mypkg/mypkg.go, mypkg/a.go, mypkg/b.go, pkg2/c.go).
	sort.Strings(paths)
	want := []string{"mypkg/a.go", "mypkg/b.go", "mypkg/mypkg.go", "pkg2/c.go"}
	if len(paths) != len(want) {
		t.Fatalf("DistinctFilePaths len = %d, want %d (got %v)", len(paths), len(want), paths)
	}
	for i, p := range want {
		if paths[i] != p {
			t.Errorf("DistinctFilePaths[%d] = %q, want %q", i, paths[i], p)
		}
	}
}

func TestDistinctFilePaths_OtherLang(t *testing.T) {
	s := newFixtureStore(t)

	paths, err := s.DistinctFilePaths("ts")
	if err != nil {
		t.Fatalf("DistinctFilePaths(ts): %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("expected 0 ts paths, got %v", paths)
	}
}

// ---------------------------------------------------------------------------
// TestQueryNodes
// ---------------------------------------------------------------------------

func TestQueryNodes_Package(t *testing.T) {
	s := newFixtureStore(t)

	// Empty parent → returns Package-type nodes only.
	nodes, err := s.QueryNodes("", 100)
	if err != nil {
		t.Fatalf("QueryNodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 Package node, got %d", len(nodes))
	}
	if nodes[0].ID != "pkg_000000000000" {
		t.Errorf("expected pkg node, got %q", nodes[0].ID)
	}
}

func TestQueryNodes_LimitRespected(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "limit.db")
	s, err := persist.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	pkgs := []types.Node{
		makeNode("pkg1000000000000", types.NodePackage, "pkg1", "pkg1"),
		makeNode("pkg2000000000000", types.NodePackage, "pkg2", "pkg2"),
		makeNode("pkg3000000000000", types.NodePackage, "pkg3", "pkg3"),
	}
	if err := s.InsertNodes(pkgs); err != nil {
		t.Fatalf("InsertNodes: %v", err)
	}

	nodes, err := s.QueryNodes("", 2)
	if err != nil {
		t.Fatalf("QueryNodes: %v", err)
	}
	if len(nodes) > 2 {
		t.Errorf("limit=2 returned %d nodes", len(nodes))
	}
}

// ---------------------------------------------------------------------------
// TestTopNodes
// ---------------------------------------------------------------------------

func TestTopNodes_PageRankDescOrder(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "top.db")
	s, err := persist.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	mk := func(id string, pr, us float64) types.Node {
		n := makeNode(id, types.NodeFunction, id, "pkg."+id)
		n.PageRank = pr
		n.UsageScore = us
		return n
	}
	if err := s.InsertNodes([]types.Node{
		mk("aa00000000000001", 0.10, 5),
		mk("bb00000000000001", 0.50, 1),
		mk("cc00000000000001", 0.30, 9),
	}); err != nil {
		t.Fatalf("InsertNodes: %v", err)
	}

	got, err := s.TopNodes("pagerank", 10)
	if err != nil {
		t.Fatalf("TopNodes pagerank: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d nodes, want 3", len(got))
	}
	if got[0].ID != "bb00000000000001" || got[1].ID != "cc00000000000001" || got[2].ID != "aa00000000000001" {
		t.Errorf("pagerank order = [%s,%s,%s], want [bb,cc,aa]",
			got[0].ID, got[1].ID, got[2].ID)
	}

	got2, err := s.TopNodes("usage", 10)
	if err != nil {
		t.Fatalf("TopNodes usage: %v", err)
	}
	if got2[0].ID != "cc00000000000001" || got2[1].ID != "aa00000000000001" || got2[2].ID != "bb00000000000001" {
		t.Errorf("usage order = [%s,%s,%s], want [cc,aa,bb]",
			got2[0].ID, got2[1].ID, got2[2].ID)
	}
}

func TestTopNodes_LimitRespected(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "top_limit.db")
	s, err := persist.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	nodes := make([]types.Node, 5)
	for i := range nodes {
		n := makeNode("nn0000000000000"+string(rune('1'+i)),
			types.NodeFunction, "n", "pkg.n")
		n.PageRank = float64(i)
		nodes[i] = n
	}
	if err := s.InsertNodes(nodes); err != nil {
		t.Fatalf("InsertNodes: %v", err)
	}
	got, err := s.TopNodes("pagerank", 2)
	if err != nil {
		t.Fatalf("TopNodes: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("limit=2 returned %d nodes", len(got))
	}
}

func TestTopNodes_InvalidMetric(t *testing.T) {
	s := newFixtureStore(t)
	_, err := s.TopNodes("bogus", 10)
	if err == nil {
		t.Fatalf("expected error for invalid metric, got nil")
	}
	if !errors.Is(err, persist.ErrInvalidMetric) {
		t.Errorf("error = %v; want ErrInvalidMetric", err)
	}
}

// makeNode is a minimal factory for package-type test nodes.
func makeNode(id string, nt types.NodeType, name, qname string) types.Node {
	return types.Node{
		ID:            id,
		Type:          nt,
		Name:          name,
		QualifiedName: qname,
		FilePath:      "x/x.go",
		StartLine:     1, EndLine: 2, StartByte: 0, EndByte: 10,
		Language:   "go",
		Confidence: types.ConfExtracted,
	}
}

// ---------------------------------------------------------------------------
// TestQueryEdgesByType
// ---------------------------------------------------------------------------

func TestQueryEdgesByType_Calls(t *testing.T) {
	s := newFixtureStore(t)

	edges, err := s.QueryEdgesByType(string(types.EdgeCalls))
	if err != nil {
		t.Fatalf("QueryEdgesByType: %v", err)
	}
	if len(edges) != 2 {
		t.Fatalf("expected 2 calls edges, got %d", len(edges))
	}
	for _, e := range edges {
		if e.Type != types.EdgeCalls {
			t.Errorf("unexpected edge type %q", e.Type)
		}
	}
}

func TestQueryEdgesByType_Contains(t *testing.T) {
	s := newFixtureStore(t)

	edges, err := s.QueryEdgesByType(string(types.EdgeContains))
	if err != nil {
		t.Fatalf("QueryEdgesByType: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 contains edge, got %d", len(edges))
	}
	if edges[0].Src != "pkg_000000000000" || edges[0].Dst != "funcA00000000000" {
		t.Errorf("contains edge wrong endpoints: %+v", edges[0])
	}
}

func TestQueryEdgesByType_NoMatch(t *testing.T) {
	s := newFixtureStore(t)

	edges, err := s.QueryEdgesByType(string(types.EdgeImplements))
	if err != nil {
		t.Fatalf("QueryEdgesByType: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(edges))
	}
}

// ---------------------------------------------------------------------------
// TestQueryEdgesForNodes
// ---------------------------------------------------------------------------

func TestQueryEdgesForNodes_TouchingA(t *testing.T) {
	s := newFixtureStore(t)

	// funcA is src of calls(A→B) and dst of contains(pkg→A).
	edges, err := s.QueryEdgesForNodes([]string{"funcA00000000000"})
	if err != nil {
		t.Fatalf("QueryEdgesForNodes: %v", err)
	}
	if len(edges) != 2 {
		t.Fatalf("expected 2 edges touching funcA, got %d: %+v", len(edges), edges)
	}
}

func TestQueryEdgesForNodes_MultipleNodes(t *testing.T) {
	s := newFixtureStore(t)

	// Both A and B → should return all 3 edges (contains+calls+calls).
	edges, err := s.QueryEdgesForNodes([]string{"funcA00000000000", "funcB00000000000"})
	if err != nil {
		t.Fatalf("QueryEdgesForNodes: %v", err)
	}
	if len(edges) != 3 {
		t.Fatalf("expected 3 edges touching A+B, got %d: %+v", len(edges), edges)
	}
}

func TestQueryEdgesForNodes_Empty(t *testing.T) {
	s := newFixtureStore(t)

	edges, err := s.QueryEdgesForNodes(nil)
	if err != nil {
		t.Fatalf("QueryEdgesForNodes(nil): %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges for nil input, got %d", len(edges))
	}
}

// ---------------------------------------------------------------------------
// TestGetBlob
// ---------------------------------------------------------------------------

func TestGetBlob_Exists(t *testing.T) {
	s := newFixtureStore(t)

	b, err := s.GetBlob("funcA00000000000")
	if err != nil {
		t.Fatalf("GetBlob(funcA): %v", err)
	}
	if len(b) == 0 {
		t.Errorf("expected non-empty blob for funcA")
	}
}

func TestGetBlob_Missing(t *testing.T) {
	s := newFixtureStore(t)

	// funcB has no blob in our fixture.
	b, err := s.GetBlob("funcB00000000000")
	if err == nil {
		t.Errorf("expected error for missing blob, got nil (blob=%q)", b)
	}
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
	if b != nil {
		t.Errorf("expected nil bytes for missing blob, got %q", b)
	}
}

// ---------------------------------------------------------------------------
// TestSearchFTS
// ---------------------------------------------------------------------------

func TestSearchFTS_Hit(t *testing.T) {
	s := newFixtureStore(t)

	// "FuncA" is stored in the name column and in doc_comment.
	hits, err := s.SearchFTS("FuncA", 10, persist.SearchFTSOptions{})
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least 1 FTS hit for 'FuncA', got 0")
	}
	ids := make([]string, len(hits))
	for i, h := range hits {
		ids[i] = h.Node.ID
	}
	if !containsID(ids, "funcA00000000000") {
		t.Errorf("FTS hit set %v does not contain funcA", ids)
	}
}

func TestSearchFTS_NoMatch(t *testing.T) {
	s := newFixtureStore(t)

	hits, err := s.SearchFTS("zzzzzz_no_match", 10, persist.SearchFTSOptions{})
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected 0 FTS hits for nonsense query, got %d", len(hits))
	}
}

func TestSearchFTS_LimitRespected(t *testing.T) {
	s := newFixtureStore(t)

	// "mypkg" matches the name of Package and the qualified_name prefix of A and B.
	// The limit=1 should cap results.
	hits, err := s.SearchFTS("mypkg*", 1, persist.SearchFTSOptions{})
	if err != nil {
		t.Fatalf("SearchFTS(limit=1): %v", err)
	}
	if len(hits) > 1 {
		t.Errorf("limit=1 returned %d results", len(hits))
	}
}

// ---------------------------------------------------------------------------
// TestFindSymbol
// ---------------------------------------------------------------------------

func TestFindSymbol_ExactMatch(t *testing.T) {
	s := newFixtureStore(t)

	nodes, err := s.FindSymbol("mypkg.FuncA", true, persist.FindSymbolOptions{})
	if err != nil {
		t.Fatalf("FindSymbol exact: %v", err)
	}
	if len(nodes) != 1 || nodes[0].ID != "funcA00000000000" {
		t.Errorf("expected funcA, got %+v", nodes)
	}
}

func TestFindSymbol_SuffixMatch(t *testing.T) {
	s := newFixtureStore(t)

	// Suffix match: "FuncB" should hit "mypkg.FuncB".
	nodes, err := s.FindSymbol("FuncB", false, persist.FindSymbolOptions{})
	if err != nil {
		t.Fatalf("FindSymbol suffix: %v", err)
	}
	if len(nodes) == 0 {
		t.Fatal("expected suffix match for 'FuncB', got 0 results")
	}
	ids := nodeIDs(nodes)
	if !containsID(ids, "funcB00000000000") {
		t.Errorf("suffix match result %v does not contain funcB", ids)
	}
}

// TestFindSymbol_SuffixMatchCaseInsensitive locks the contract that suffix
// matching is case-insensitive, as the original leading-wildcard LIKE was.
// The simple_name equi-join must use COLLATE NOCASE so a lowercase query still
// hits a CamelCase symbol (e.g. "funcb" → "mypkg.FuncB").
func TestFindSymbol_SuffixMatchCaseInsensitive(t *testing.T) {
	s := newFixtureStore(t)

	nodes, err := s.FindSymbol("funcb", false, persist.FindSymbolOptions{})
	if err != nil {
		t.Fatalf("FindSymbol case-insensitive suffix: %v", err)
	}
	if !containsID(nodeIDs(nodes), "funcB00000000000") {
		t.Errorf("case-insensitive suffix 'funcb' did not hit mypkg.FuncB: %v", nodeIDs(nodes))
	}
}

func TestFindSymbol_WithLanguageFilter(t *testing.T) {
	s := newFixtureStore(t)

	nodes, err := s.FindSymbol("FuncA", false, persist.FindSymbolOptions{Language: "go"})
	if err != nil {
		t.Fatalf("FindSymbol+lang: %v", err)
	}
	if len(nodes) == 0 {
		t.Fatal("expected at least 1 result with language=go")
	}
	for _, n := range nodes {
		if n.Language != "go" {
			t.Errorf("language filter failed: got language=%q", n.Language)
		}
	}
}

func TestFindSymbol_NoMatch(t *testing.T) {
	s := newFixtureStore(t)

	nodes, err := s.FindSymbol("DoesNotExist", true, persist.FindSymbolOptions{})
	if err != nil {
		t.Fatalf("FindSymbol no-match: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 results for non-existent symbol, got %d", len(nodes))
	}
}

// ---------------------------------------------------------------------------
// TestNeighborhoodByQname
// ---------------------------------------------------------------------------

func TestNeighborhoodByQname_Forward_Depth1(t *testing.T) {
	s := newFixtureStore(t)

	// From funcA, depth=1: should reach funcB (calls A→B).
	nodes, edges, err := s.NeighborhoodByQname("mypkg.FuncA", 1, false)
	if err != nil {
		t.Fatalf("NeighborhoodByQname fwd d1: %v", err)
	}
	ids := nodeIDs(nodes)
	if !containsID(ids, "funcA00000000000") {
		t.Errorf("seed node funcA not in result: %v", ids)
	}
	if !containsID(ids, "funcB00000000000") {
		t.Errorf("funcB not in depth-1 forward result: %v", ids)
	}
	if containsID(ids, "funcC00000000000") {
		t.Errorf("funcC should not be in depth-1 forward result: %v", ids)
	}
	if len(edges) == 0 {
		t.Error("expected at least 1 edge in neighborhood")
	}
}

func TestNeighborhoodByQname_Forward_Depth2(t *testing.T) {
	s := newFixtureStore(t)

	// From funcA, depth=2: should reach funcB and funcC (A→B→C).
	nodes, _, err := s.NeighborhoodByQname("mypkg.FuncA", 2, false)
	if err != nil {
		t.Fatalf("NeighborhoodByQname fwd d2: %v", err)
	}
	ids := nodeIDs(nodes)
	if !containsID(ids, "funcC00000000000") {
		t.Errorf("funcC should be in depth-2 forward result: %v", ids)
	}
}

func TestNeighborhoodByQname_Reverse_Callers(t *testing.T) {
	s := newFixtureStore(t)

	// From funcC reverse: B calls C, A calls B — depth=2 reaches both callers.
	nodes, edges, err := s.NeighborhoodByQname("pkg2.FuncC", 2, true)
	if err != nil {
		t.Fatalf("NeighborhoodByQname rev: %v", err)
	}
	ids := nodeIDs(nodes)
	if !containsID(ids, "funcC00000000000") {
		t.Errorf("seed funcC not in result: %v", ids)
	}
	if !containsID(ids, "funcB00000000000") {
		t.Errorf("funcB (direct caller) not in reverse result: %v", ids)
	}
	if !containsID(ids, "funcA00000000000") {
		t.Errorf("funcA (indirect caller) not in depth-2 reverse result: %v", ids)
	}
	if len(edges) == 0 {
		t.Error("expected edges in reverse neighborhood")
	}
}

func TestNeighborhoodByQname_NotFound(t *testing.T) {
	s := newFixtureStore(t)

	nodes, edges, err := s.NeighborhoodByQname("nonexistent.Sym", 3, false)
	if err != nil {
		t.Fatalf("NeighborhoodByQname not-found: %v", err)
	}
	if len(nodes) != 0 || len(edges) != 0 {
		t.Errorf("expected empty result for non-existent qname, got nodes=%d edges=%d", len(nodes), len(edges))
	}
}

// ---------------------------------------------------------------------------
// TestSubgraphByQname
// ---------------------------------------------------------------------------

func TestSubgraphByQname_Depth1(t *testing.T) {
	s := newFixtureStore(t)

	// BFS at funcA depth=1: forward reaches B, reverse reaches pkg (via contains).
	nodes, edges, err := s.SubgraphByQname("mypkg.FuncA", 1)
	if err != nil {
		t.Fatalf("SubgraphByQname d1: %v", err)
	}
	ids := nodeIDs(nodes)
	if !containsID(ids, "funcA00000000000") {
		t.Errorf("funcA not in subgraph: %v", ids)
	}
	if !containsID(ids, "funcB00000000000") {
		t.Errorf("funcB not in depth-1 subgraph: %v", ids)
	}
	if len(edges) == 0 {
		t.Error("expected edges in subgraph")
	}
}

func TestSubgraphByQname_FullGraph(t *testing.T) {
	s := newFixtureStore(t)

	// At depth=99, BFS should find all 4 nodes.
	nodes, _, err := s.SubgraphByQname("mypkg.FuncA", 99)
	if err != nil {
		t.Fatalf("SubgraphByQname full: %v", err)
	}
	if len(nodes) < 4 {
		t.Errorf("expected ≥4 nodes for full BFS, got %d: %v", len(nodes), nodeIDs(nodes))
	}
}

func TestSubgraphByQname_NotFound(t *testing.T) {
	s := newFixtureStore(t)

	nodes, edges, err := s.SubgraphByQname("no.Such", 5)
	if err != nil {
		t.Fatalf("SubgraphByQname not-found: %v", err)
	}
	if len(nodes) != 0 || len(edges) != 0 {
		t.Errorf("expected empty result for non-existent qname, got nodes=%d edges=%d", len(nodes), len(edges))
	}
}

// ---------------------------------------------------------------------------
// TestNodesByIDs
// ---------------------------------------------------------------------------

func TestNodesByIDs_AllValid(t *testing.T) {
	s := newFixtureStore(t)

	nodes, err := s.NodesByIDs([]string{"funcA00000000000", "funcB00000000000"})
	if err != nil {
		t.Fatalf("NodesByIDs: %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(nodes))
	}
}

func TestNodesByIDs_MixedValidInvalid(t *testing.T) {
	s := newFixtureStore(t)

	nodes, err := s.NodesByIDs([]string{"funcA00000000000", "DOESNOTEXIST0000"})
	if err != nil {
		t.Fatalf("NodesByIDs: %v", err)
	}
	// Only the valid ID should be returned.
	if len(nodes) != 1 {
		t.Errorf("expected 1 node for mixed valid/invalid IDs, got %d", len(nodes))
	}
	if nodes[0].ID != "funcA00000000000" {
		t.Errorf("expected funcA, got %q", nodes[0].ID)
	}
}

func TestNodesByIDs_Empty(t *testing.T) {
	s := newFixtureStore(t)

	nodes, err := s.NodesByIDs(nil)
	if err != nil {
		t.Fatalf("NodesByIDs(nil): %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected nil/empty result for empty input, got %d", len(nodes))
	}
}

func TestNodesByIDs_AllInvalid(t *testing.T) {
	s := newFixtureStore(t)

	nodes, err := s.NodesByIDs([]string{"DOESNOTEXIST0000", "ALSOMISSING00000"})
	if err != nil {
		t.Fatalf("NodesByIDs all-invalid: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes for all-invalid IDs, got %d", len(nodes))
	}
}

// ---------------------------------------------------------------------------
// A3 incremental cache: NodesByFilePath / EdgesByFilePath / BlobsByFilePath
// + DeleteNodesByFilePath FK CASCADE behavior
// ---------------------------------------------------------------------------

// TestNodesByFilePath_Hit verifies the per-file lookup returns the exact set
// of nodes whose file_path matches and nothing else. funcA lives in
// "mypkg/a.go" alone in the fixture, so the result is exactly one node.
func TestNodesByFilePath_Hit(t *testing.T) {
	s := newFixtureStore(t)
	nodes, err := s.NodesByFilePath("mypkg/a.go")
	if err != nil {
		t.Fatalf("NodesByFilePath: %v", err)
	}
	if len(nodes) != 1 || nodes[0].ID != "funcA00000000000" {
		t.Errorf("expected [funcA], got %v", nodeIDs(nodes))
	}
}

// TestNodesByFilePath_Empty verifies path with no rows returns empty slice
// (not error). Empty input path returns nil without DB hit.
func TestNodesByFilePath_Empty(t *testing.T) {
	s := newFixtureStore(t)
	nodes, err := s.NodesByFilePath("does/not/exist.go")
	if err != nil {
		t.Fatalf("NodesByFilePath empty: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(nodes))
	}
	nodes, err = s.NodesByFilePath("")
	if err != nil || nodes != nil {
		t.Errorf("empty path should return (nil,nil), got (%v,%v)", nodes, err)
	}
}

// TestEdgesByFilePath needs an edge with a file_path. The fixture's edges
// don't have one, so we insert a dedicated edge here.
func TestEdgesByFilePath_Hit(t *testing.T) {
	s := newFixtureStore(t)
	if err := s.InsertEdges([]types.Edge{{
		Src: "funcA00000000000", Dst: "funcB00000000000", Type: types.EdgeCalls,
		FilePath: "mypkg/a.go", Line: 5, Count: 1, Confidence: types.ConfExtracted,
	}}); err != nil {
		t.Fatalf("InsertEdges: %v", err)
	}
	got, err := s.EdgesByFilePath("mypkg/a.go")
	if err != nil {
		t.Fatalf("EdgesByFilePath: %v", err)
	}
	if len(got) != 1 || got[0].FilePath != "mypkg/a.go" {
		t.Errorf("expected 1 edge for mypkg/a.go, got %+v", got)
	}
	if got[0].ID == 0 {
		t.Errorf("expected non-zero edge ID, got %d", got[0].ID)
	}
}

// TestBlobsByFilePath verifies blobs are returned keyed by node_id, scoped to
// only the nodes living in path. funcA has a blob and lives in mypkg/a.go;
// funcB has no blob; funcC is in pkg2/c.go.
func TestBlobsByFilePath(t *testing.T) {
	s := newFixtureStore(t)
	got, err := s.BlobsByFilePath("mypkg/a.go")
	if err != nil {
		t.Fatalf("BlobsByFilePath: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 blob for mypkg/a.go, got %d", len(got))
	}
	if _, ok := got["funcA00000000000"]; !ok {
		t.Errorf("expected funcA blob, got keys %v", mapKeysForTest(got))
	}
	// Non-blob file returns empty map (not nil).
	got2, err := s.BlobsByFilePath("mypkg/b.go")
	if err != nil {
		t.Fatalf("BlobsByFilePath b: %v", err)
	}
	if got2 == nil {
		t.Errorf("expected non-nil empty map, got nil")
	}
	if len(got2) != 0 {
		t.Errorf("expected 0 blobs for mypkg/b.go, got %d", len(got2))
	}
}

// TestIncremental_FKCascadeOnDelete is the unit test called out in the work
// plan: insert nodes for files A and B with edges between them and a blob on
// A, delete file A's nodes, assert (a) A's nodes and blobs are gone, (b)
// edges sourcing from OR pointing to A are gone (CASCADE), (c) B's nodes
// and edges-not-touching-A survive.
func TestIncremental_FKCascadeOnDelete(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cascade.db")
	s, err := persist.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// File A: funcA. File B: funcB1, funcB2.
	nodes := []types.Node{
		{
			ID: "funcA00000000000", Type: types.NodeFunction, Name: "FuncA",
			QualifiedName: "pkg.FuncA", FilePath: "pkg/a.go",
			StartLine: 1, EndLine: 2, StartByte: 0, EndByte: 10,
			Language: "go", Confidence: types.ConfExtracted,
		},
		{
			ID: "funcB100000000000"[:16], Type: types.NodeFunction, Name: "FuncB1",
			QualifiedName: "pkg.FuncB1", FilePath: "pkg/b.go",
			StartLine: 1, EndLine: 2, StartByte: 0, EndByte: 10,
			Language: "go", Confidence: types.ConfExtracted,
		},
		{
			ID: "funcB200000000000"[:16], Type: types.NodeFunction, Name: "FuncB2",
			QualifiedName: "pkg.FuncB2", FilePath: "pkg/b.go",
			StartLine: 3, EndLine: 4, StartByte: 11, EndByte: 20,
			Language: "go", Confidence: types.ConfExtracted,
		},
	}
	if err := s.InsertNodes(nodes); err != nil {
		t.Fatalf("InsertNodes: %v", err)
	}
	// B1 → A and B1 → B2. Deleting A's nodes must cascade B1→A but leave B1→B2.
	edges := []types.Edge{
		{Src: "funcB100000000000"[:16], Dst: "funcA00000000000",
			Type: types.EdgeCalls, FilePath: "pkg/b.go",
			Count: 1, Confidence: types.ConfExtracted},
		{Src: "funcB100000000000"[:16], Dst: "funcB200000000000"[:16],
			Type: types.EdgeCalls, FilePath: "pkg/b.go",
			Count: 1, Confidence: types.ConfExtracted},
	}
	if err := s.InsertEdges(edges); err != nil {
		t.Fatalf("InsertEdges: %v", err)
	}
	if err := s.InsertBlobs(map[string][]byte{
		"funcA00000000000": []byte("body A"),
	}); err != nil {
		t.Fatalf("InsertBlobs: %v", err)
	}

	// Sanity: 3 nodes, 2 edges, 1 blob exist before delete.
	if got, _ := s.NodesByFilePath("pkg/a.go"); len(got) != 1 {
		t.Fatalf("pre-delete pkg/a.go nodes = %d, want 1", len(got))
	}
	if got, _ := s.EdgesByFilePath("pkg/b.go"); len(got) != 2 {
		t.Fatalf("pre-delete pkg/b.go edges = %d, want 2", len(got))
	}

	// Delete file A's nodes.
	if err := s.DeleteNodesByFilePath("pkg/a.go"); err != nil {
		t.Fatalf("DeleteNodesByFilePath: %v", err)
	}

	// (a) A's nodes gone.
	if got, _ := s.NodesByFilePath("pkg/a.go"); len(got) != 0 {
		t.Errorf("post-delete pkg/a.go nodes = %d, want 0", len(got))
	}
	// (a) A's blob cascaded.
	if _, err := s.GetBlob("funcA00000000000"); err == nil {
		t.Errorf("expected blob delete to cascade, but blob still present")
	}
	// (b) Edge B1→A cascaded; edge B1→B2 survives.
	bEdges, err := s.EdgesByFilePath("pkg/b.go")
	if err != nil {
		t.Fatalf("EdgesByFilePath post: %v", err)
	}
	if len(bEdges) != 1 {
		t.Errorf("post-delete pkg/b.go edges = %d, want 1 (B1→A cascaded, B1→B2 survives): %+v",
			len(bEdges), bEdges)
	}
	if len(bEdges) == 1 && bEdges[0].Dst != "funcB200000000000"[:16] {
		t.Errorf("survivor edge dst = %q, want funcB2", bEdges[0].Dst)
	}
	// (c) File B's nodes intact.
	bNodes, _ := s.NodesByFilePath("pkg/b.go")
	if len(bNodes) != 2 {
		t.Errorf("post-delete pkg/b.go nodes = %d, want 2", len(bNodes))
	}
}

// TestReverseDepsForFiles verifies the C1 reverse-reference query:
// cached files whose pending_refs target qnames defined in dirty files are
// returned, matching via both exact and suffix (simpleName) join.
func TestReverseDepsForFiles(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "revdeps.db")
	s, err := persist.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Two files: dirty.go defines Helper; cached.go calls Helper (pending ref).
	nodes := []types.Node{
		{
			ID: "dirty0000000000a", Type: types.NodeFunction,
			Name: "Helper", QualifiedName: "mypkg.Helper",
			FilePath: "dirty.go", Language: "go",
			StartLine: 1, EndLine: 5, StartByte: 0, EndByte: 50,
			Confidence: types.ConfExtracted,
		},
		{
			ID: "cache0000000000b", Type: types.NodeFunction,
			Name: "Use", QualifiedName: "mypkg.Use",
			FilePath: "cached.go", Language: "go",
			StartLine: 1, EndLine: 5, StartByte: 0, EndByte: 50,
			Confidence: types.ConfExtracted,
		},
		{
			ID: "unrel0000000000c", Type: types.NodeFunction,
			Name: "Unrelated", QualifiedName: "other.Unrelated",
			FilePath: "other.go", Language: "go",
			StartLine: 1, EndLine: 5, StartByte: 0, EndByte: 50,
			Confidence: types.ConfExtracted,
		},
	}
	if err := s.InsertNodes(nodes); err != nil {
		t.Fatalf("InsertNodes: %v", err)
	}

	// pending_refs: cached.go has a ref targeting "Helper" (simple name, as
	// the parser emits before resolution) — mirrors exprName() in Go parser.
	// other.go has a ref targeting "Unrelated" (not in dirty.go).
	refs := []persist.PendingRefRow{
		{FilePath: "cached.go", SrcID: "cache0000000000b", EdgeType: "calls", TargetQName: "Helper"},
		{FilePath: "other.go", SrcID: "unrel0000000000c", EdgeType: "calls", TargetQName: "Unrelated"},
	}
	if err := s.InsertPendingRefs(refs); err != nil {
		t.Fatalf("InsertPendingRefs: %v", err)
	}

	// Empty dirtyPaths → nil result, no error.
	got, err := s.ReverseDepsForFiles(nil)
	if err != nil || got != nil {
		t.Errorf("empty dirtyPaths: got %v, %v; want nil, nil", got, err)
	}

	// dirty.go is dirty → cached.go should be returned (suffix match: "Helper" in "mypkg.Helper").
	// other.go should NOT be returned ("Unrelated" is not defined in dirty.go).
	got, err = s.ReverseDepsForFiles([]string{"dirty.go"})
	if err != nil {
		t.Fatalf("ReverseDepsForFiles: %v", err)
	}
	if len(got) != 1 || got[0] != "cached.go" {
		t.Errorf("ReverseDepsForFiles([dirty.go]) = %v; want [cached.go]", got)
	}
}

// mapKeysForTest is a sortable-key helper used by the BlobsByFilePath
// assertion to keep the diagnostic message readable.
func mapKeysForTest(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
