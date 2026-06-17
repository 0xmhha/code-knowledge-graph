package persist

import (
	"database/sql"
	"testing"
	"time"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// mockStoreReader is a test double for StoreReader. It returns a fixed set
// of nodes, edges and blobs so tests can exercise loadAllNodes and edgeKey
// without a real SQLite or PostgreSQL connection.
type mockStoreReader struct {
	// nodesByLang maps language → file_path → []Node for DistinctFilePaths
	// and NodesByFilePath simulation.
	nodesByLang map[string]map[string][]types.Node
	// edges returned by QueryEdgesByType (keyed by type string).
	edgesByType map[string][]types.Edge
	// blobs returned by GetBlob (keyed by node ID; missing means ErrNoRows).
	blobs map[string][]byte
}

func (m *mockStoreReader) Close() error { return nil }
func (m *mockStoreReader) GetManifest() (Manifest, error) {
	return Manifest{}, nil
}
func (m *mockStoreReader) LoadHierarchy(_ string) ([]HierarchyRow, error) { return nil, nil }
func (m *mockStoreReader) FindSymbol(_ string, _ bool, _ FindSymbolOptions) ([]types.Node, error) {
	return nil, nil
}
func (m *mockStoreReader) FindByCanonicalID(_ string) (types.Node, bool, error) {
	return types.Node{}, false, nil
}
func (m *mockStoreReader) NodesByIDs(_ []string) ([]types.Node, error) { return nil, nil }
func (m *mockStoreReader) QueryNodes(_ string, _ int) ([]types.Node, error) {
	return nil, nil
}
func (m *mockStoreReader) TopNodes(_ string, _ int, _ ...string) ([]types.Node, error) {
	return nil, nil
}
func (m *mockStoreReader) EdgeCountsByType() (map[string]int, error) {
	return nil, nil
}
func (m *mockStoreReader) DistinctFilePaths(language string) ([]string, error) {
	byFile, ok := m.nodesByLang[language]
	if !ok {
		return nil, nil
	}
	paths := make([]string, 0, len(byFile))
	for p := range byFile {
		paths = append(paths, p)
	}
	return paths, nil
}
func (m *mockStoreReader) QueryEdgesByType(t string) ([]types.Edge, error) {
	return m.edgesByType[t], nil
}
func (m *mockStoreReader) QueryEdgesForNodes(_ []string) ([]types.Edge, error) {
	return nil, nil
}
func (m *mockStoreReader) NeighborhoodByQname(_ string, _ int, _ bool, _ ...string) ([]types.Node, []types.Edge, error) {
	return nil, nil, nil
}
func (m *mockStoreReader) SubgraphByQname(_ string, _ int) ([]types.Node, []types.Edge, error) {
	return nil, nil, nil
}
func (m *mockStoreReader) Search(_ string, _ int) ([]types.Node, error) { return nil, nil }
func (m *mockStoreReader) SearchWithOpts(_ string, _ int, _ SearchFTSOptions) ([]types.Node, error) {
	return nil, nil
}
func (m *mockStoreReader) GetNodePRs(_ string, _ time.Time) ([]types.PRRef, error) {
	return nil, nil
}
func (m *mockStoreReader) SearchFTS(_ string, _ int, _ SearchFTSOptions) ([]SearchHit, error) {
	return nil, nil
}
func (m *mockStoreReader) GetBlob(id string) ([]byte, error) {
	b, ok := m.blobs[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return b, nil
}
func (m *mockStoreReader) NodesByFilePath(path string) ([]types.Node, error) {
	for _, byFile := range m.nodesByLang {
		if nodes, ok := byFile[path]; ok {
			return nodes, nil
		}
	}
	return nil, nil
}
func (m *mockStoreReader) EdgesByFilePath(_ string) ([]types.Edge, error)      { return nil, nil }
func (m *mockStoreReader) BlobsByFilePath(_ string) (map[string][]byte, error) { return nil, nil }
func (m *mockStoreReader) PendingRefsByFilePath(_ string) ([]PendingRefRow, error) {
	return nil, nil
}
func (m *mockStoreReader) ReverseDepsForFiles(_ []string) ([]string, error) { return nil, nil }
func (m *mockStoreReader) ExportChunked(_ string, _, _ int) error           { return nil }
func (m *mockStoreReader) AllNodes() ([]types.Node, error) {
	var out []types.Node
	for _, byFile := range m.nodesByLang {
		for _, ns := range byFile {
			out = append(out, ns...)
		}
	}
	return out, nil
}
func (m *mockStoreReader) AllEdges() ([]types.Edge, error)           { return nil, nil }
func (m *mockStoreReader) AmbiguousMetaNodes() ([]types.Node, error) { return nil, nil }

// Compile-time check: mockStoreReader must satisfy StoreReader.
var _ StoreReader = (*mockStoreReader)(nil)

// makeNode returns a minimal valid Node for testing.
func makeNode(id, lang, filePath string, nodeType types.NodeType) types.Node {
	return types.Node{
		ID:            id,
		Type:          nodeType,
		Name:          id,
		QualifiedName: lang + "." + id,
		FilePath:      filePath,
		StartLine:     1,
		EndLine:       2,
		StartByte:     0,
		EndByte:       10,
		Language:      lang,
		Confidence:    types.ConfExtracted,
	}
}

// TestLoadAllNodes verifies that loadAllNodes collects nodes from all known
// languages without using QueryNodes (which has the LIMIT 0 bug).
func TestLoadAllNodes(t *testing.T) {
	goNode := makeNode("go00000000000001", "go", "/repo/main.go", types.NodeFunction)
	tsNode := makeNode("ts00000000000001", "ts", "/repo/index.ts", types.NodeFunction)
	solNode := makeNode("so00000000000001", "sol", "/repo/Token.sol", types.NodeContract)

	store := &mockStoreReader{
		nodesByLang: map[string]map[string][]types.Node{
			"go":  {"/repo/main.go": {goNode}},
			"ts":  {"/repo/index.ts": {tsNode}},
			"sol": {"/repo/Token.sol": {solNode}},
		},
		edgesByType: map[string][]types.Edge{},
		blobs:       map[string][]byte{},
	}

	nodes, err := loadAllNodes(store)
	if err != nil {
		t.Fatalf("loadAllNodes: %v", err)
	}
	if got, want := len(nodes), 3; got != want {
		t.Errorf("len(nodes) = %d; want %d", got, want)
	}

	byID := make(map[string]types.Node, len(nodes))
	for _, n := range nodes {
		byID[n.ID] = n
	}
	for _, wantNode := range []types.Node{goNode, tsNode, solNode} {
		if _, ok := byID[wantNode.ID]; !ok {
			t.Errorf("node %s (%s) missing from output", wantNode.ID, wantNode.Language)
		}
	}
}

// TestLoadAllNodes_Dedup verifies that a node appearing under two edge types
// is not duplicated in the output (defensive dedup).
func TestLoadAllNodes_Dedup(t *testing.T) {
	n1 := makeNode("go00000000000001", "go", "/repo/a.go", types.NodeFunction)
	n2 := makeNode("go00000000000002", "go", "/repo/a.go", types.NodeMethod)

	store := &mockStoreReader{
		nodesByLang: map[string]map[string][]types.Node{
			"go": {"/repo/a.go": {n1, n2}},
		},
		edgesByType: map[string][]types.Edge{},
		blobs:       map[string][]byte{},
	}

	nodes, err := loadAllNodes(store)
	if err != nil {
		t.Fatalf("loadAllNodes: %v", err)
	}
	if got, want := len(nodes), 2; got != want {
		t.Errorf("len(nodes) = %d; want %d (dedup failed)", got, want)
	}
}

// TestEdgeKey verifies the composite key includes file_path to avoid
// PK collisions between edges with same src/type/dst but different file.
func TestEdgeKey(t *testing.T) {
	base := types.Edge{
		Src:        "src0000000000001",
		Dst:        "dst0000000000001",
		Type:       types.EdgeCalls,
		FilePath:   "/repo/a.go",
		Line:       0,
		Count:      1,
		Confidence: types.ConfExtracted,
	}
	other := base
	other.FilePath = "/repo/b.go"

	if edgeKey(base) == edgeKey(other) {
		t.Errorf("edgeKey collision: different file_path produced same key %q", edgeKey(base))
	}
}

// TestDSNHost verifies that DSNHost correctly extracts the host from both
// URL and key=value DSN formats without leaking credentials.
func TestDSNHost(t *testing.T) {
	cases := []struct {
		name string
		dsn  string
		want string
	}{
		{
			name: "URL format with credentials",
			dsn:  "postgres://user:secret@db.example.com/mydb",
			want: "db.example.com",
		},
		{
			name: "URL format no credentials",
			dsn:  "postgres://db.example.com/mydb",
			want: "db.example.com",
		},
		{
			name: "key=value format",
			dsn:  "host=localhost dbname=mydb user=admin",
			want: "localhost",
		},
		{
			name: "unparseable",
			dsn:  "not a dsn ://",
			want: "<unparseable>",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DSNHost(tc.dsn)
			if got != tc.want {
				t.Errorf("DSNHost(%q) = %q; want %q", tc.dsn, got, tc.want)
			}
		})
	}
}
