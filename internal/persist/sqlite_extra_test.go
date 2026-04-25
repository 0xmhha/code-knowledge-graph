package persist_test

import (
	"database/sql"
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
func newFixtureStore(t *testing.T) *persist.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "fixture.db")
	s, err := persist.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

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
	defer s.Close()
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
	results, err := s.SearchFTS("FuncA", 10)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 FTS hit for 'FuncA', got 0")
	}
	ids := nodeIDs(results)
	if !containsID(ids, "funcA00000000000") {
		t.Errorf("FTS hit set %v does not contain funcA", ids)
	}
}

func TestSearchFTS_NoMatch(t *testing.T) {
	s := newFixtureStore(t)

	results, err := s.SearchFTS("zzzzzz_no_match", 10)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 FTS hits for nonsense query, got %d", len(results))
	}
}

func TestSearchFTS_LimitRespected(t *testing.T) {
	s := newFixtureStore(t)

	// "mypkg" matches the name of Package and the qualified_name prefix of A and B.
	// The limit=1 should cap results.
	results, err := s.SearchFTS("mypkg*", 1)
	if err != nil {
		t.Fatalf("SearchFTS(limit=1): %v", err)
	}
	if len(results) > 1 {
		t.Errorf("limit=1 returned %d results", len(results))
	}
}

// ---------------------------------------------------------------------------
// TestFindSymbol
// ---------------------------------------------------------------------------

func TestFindSymbol_ExactMatch(t *testing.T) {
	s := newFixtureStore(t)

	nodes, err := s.FindSymbol("mypkg.FuncA", "", true)
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
	nodes, err := s.FindSymbol("FuncB", "", false)
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

func TestFindSymbol_WithLanguageFilter(t *testing.T) {
	s := newFixtureStore(t)

	nodes, err := s.FindSymbol("FuncA", "go", false)
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

	nodes, err := s.FindSymbol("DoesNotExist", "", true)
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
