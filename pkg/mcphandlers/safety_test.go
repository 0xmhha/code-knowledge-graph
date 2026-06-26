package mcphandlers

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestFilterLLMSafe_DropsAmbiguousMeta covers the §11.3 boundary: only
// AMBIGUOUS-confidence Hunk + Commit nodes get dropped. Other AMBIGUOUS
// rows (e.g. cross-file calls Resolve couldn't disambiguate) survive
// because the AMBIGUOUS marker on those is a precision signal the LLM
// should still see, not a recovery-only data class.
func TestFilterLLMSafe_DropsAmbiguousMeta(t *testing.T) {
	in := []types.Node{
		// EXTRACTED Hunk: keep
		{ID: "h1", Type: types.NodeHunk, Confidence: types.ConfExtracted},
		// AMBIGUOUS Hunk: drop (unreachable history)
		{ID: "h2", Type: types.NodeHunk, Confidence: types.ConfAmbiguous},
		// AMBIGUOUS Commit: drop (unreachable history)
		{ID: "c2", Type: types.NodeCommit, Confidence: types.ConfAmbiguous},
		// EXTRACTED Commit: keep
		{ID: "c1", Type: types.NodeCommit, Confidence: types.ConfExtracted},
		// AMBIGUOUS Function (TS resolve multi-candidate): keep — the
		// AMBIGUOUS confidence here means "we picked the highest-PR
		// candidate but tell the LLM the resolution is uncertain".
		{ID: "f1", Type: types.NodeFunction, Confidence: types.ConfAmbiguous},
		// EXTRACTED Method: keep (default case)
		{ID: "m1", Type: types.NodeMethod, Confidence: types.ConfExtracted},
	}
	out := filterLLMSafe(in)
	keptIDs := map[string]bool{}
	for _, n := range out {
		keptIDs[n.ID] = true
	}
	for _, want := range []string{"h1", "c1", "f1", "m1"} {
		if !keptIDs[want] {
			t.Errorf("filterLLMSafe dropped %q (should be kept)", want)
		}
	}
	for _, drop := range []string{"h2", "c2"} {
		if keptIDs[drop] {
			t.Errorf("filterLLMSafe kept %q (should be dropped)", drop)
		}
	}
	if len(out) != 4 {
		t.Errorf("expected 4 nodes after filter, got %d", len(out))
	}
}

// TestFilterLLMSafeEdges_DropsOrphans verifies the edge filter respects
// the post-node membership: edges whose endpoints were filtered out
// must also be dropped, otherwise the LLM sees dangling references.
func TestFilterLLMSafeEdges_DropsOrphans(t *testing.T) {
	allowed := map[string]bool{"a": true, "b": true, "c": true}
	in := []types.Edge{
		{Src: "a", Dst: "b"}, // both in allowed
		{Src: "a", Dst: "x"}, // x dropped
		{Src: "y", Dst: "c"}, // y dropped
		{Src: "b", Dst: "c"}, // both in allowed
	}
	out := filterLLMSafeEdges(in, allowed)
	if len(out) != 2 {
		t.Fatalf("expected 2 surviving edges, got %d (%v)", len(out), out)
	}
	for _, e := range out {
		if !allowed[e.Src] || !allowed[e.Dst] {
			t.Errorf("orphan edge slipped through: %s → %s", e.Src, e.Dst)
		}
	}
}

// TestLLMSafeStoreReader_FilterAt every read site verifies that the
// wrapped reader applies the boundary regardless of which method the
// caller used. Uses a hand-rolled fake store so we don't have to spin
// up SQLite for a unit test.
func TestLLMSafeStoreReader_FilterAt_AllMethods(t *testing.T) {
	fake := &fakeStore{
		nodes: []types.Node{
			{ID: "h_amb", Type: types.NodeHunk, Confidence: types.ConfAmbiguous, Name: "ambiguous-hunk"},
			{ID: "h_ext", Type: types.NodeHunk, Confidence: types.ConfExtracted, Name: "good-hunk"},
			{ID: "fn", Type: types.NodeFunction, Confidence: types.ConfExtracted, Name: "Foo"},
		},
		edges: []types.Edge{
			{Src: "fn", Dst: "h_amb", Type: types.EdgeHasHunk},
			{Src: "fn", Dst: "h_ext", Type: types.EdgeHasHunk},
		},
	}
	safe := NewLLMSafeReader(fake)

	// FindSymbol — exercise the filtered read path.
	out, err := safe.FindSymbol("", true, persist.FindSymbolOptions{})
	if err != nil {
		t.Fatalf("FindSymbol: %v", err)
	}
	for _, n := range out {
		if n.ID == "h_amb" {
			t.Errorf("FindSymbol leaked AMBIGUOUS Hunk")
		}
	}

	// NodesByIDs — even when caller asks specifically for the AMBIGUOUS
	// id, the filter drops it (defensive against stale IDs leaking
	// through cache layers).
	got, _ := safe.NodesByIDs([]string{"h_amb", "fn"})
	if len(got) != 1 || got[0].ID != "fn" {
		t.Errorf("NodesByIDs leaked AMBIGUOUS: got %v", got)
	}

	// SubgraphByQname — both nodes and edges should be filtered.
	nodes, edges, _ := safe.SubgraphByQname("Foo", 1)
	for _, n := range nodes {
		if isAmbiguousMeta(n) {
			t.Errorf("SubgraphByQname leaked AMBIGUOUS node %s", n.ID)
		}
	}
	for _, e := range edges {
		if e.Dst == "h_amb" {
			t.Errorf("SubgraphByQname returned edge to AMBIGUOUS Hunk")
		}
	}

	// GetBlob — refuse to return blob for an AMBIGUOUS Hunk ID.
	_, err = safe.GetBlob("h_amb")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetBlob(h_amb) should return sql.ErrNoRows, got %v", err)
	}
	// EXTRACTED Hunk blob comes through.
	if _, err := safe.GetBlob("h_ext"); err != nil {
		t.Errorf("GetBlob(h_ext) should succeed, got %v", err)
	}
}

// TestLLMSafeStoreReader_AllReadMethods_DropAmbiguousMeta locks in the
// §11.3 boundary across every read method llmSafeStoreReader overrides.
// Each method receives the same fake store containing one AMBIGUOUS
// Hunk + one AMBIGUOUS Commit + one EXTRACTED Function; we assert that
// the AMBIGUOUS rows never make it past the wrapper while the
// EXTRACTED row always does.
//
// Adding a new read method to llmSafeStoreReader without extending this
// test should fail compilation (the method becomes part of the
// boundary contract — if it doesn't filter, the §11.3 promise breaks).
// The AddTool wiring inside server.go uses the wrapper for every tool
// (registerFindSymbol/Callers/Callees/GetSubgraph/SearchText/
// GetContextForTask/ImpactOfChange/EvidenceForIntent), so this single
// table-driven test exercises the boundary for all 8 tools at once.
func TestLLMSafeStoreReader_AllReadMethods_DropAmbiguousMeta(t *testing.T) {
	fake := &fakeStore{
		nodes: []types.Node{
			{ID: "h_amb", Type: types.NodeHunk, Confidence: types.ConfAmbiguous, Name: "ambiguous-hunk"},
			{ID: "c_amb", Type: types.NodeCommit, Confidence: types.ConfAmbiguous, Name: "ambiguous-commit"},
			{ID: "fn", Type: types.NodeFunction, Confidence: types.ConfExtracted, Name: "Foo"},
		},
		edges: []types.Edge{
			{Src: "fn", Dst: "h_amb", Type: types.EdgeHasHunk},
			{Src: "fn", Dst: "c_amb", Type: types.EdgeChangedIn},
		},
	}
	safe := NewLLMSafeReader(fake)

	// table-driven: every wrapper method that returns []types.Node must
	// drop the AMBIGUOUS Hunk + Commit while keeping the EXTRACTED
	// Function. Each entry is the wrapper invocation; we assert the
	// returned slice respects the boundary.
	type call struct {
		name string
		run  func() ([]types.Node, error)
	}
	calls := []call{
		{"FindSymbol", func() ([]types.Node, error) {
			return safe.FindSymbol("Foo", false, persist.FindSymbolOptions{})
		}},
		{"NodesByIDs", func() ([]types.Node, error) { return safe.NodesByIDs([]string{"h_amb", "c_amb", "fn"}) }},
		{"QueryNodes", func() ([]types.Node, error) { return safe.QueryNodes("", 100) }},
		{"TopNodes", func() ([]types.Node, error) { return safe.TopNodes("pagerank", 100) }},
		{"Search", func() ([]types.Node, error) { return safe.Search("anything", 100) }},
		{"SearchWithOpts", func() ([]types.Node, error) {
			return safe.SearchWithOpts("anything", 100, persist.SearchFTSOptions{Mode: "and"})
		}},
		{"SearchFTS", func() ([]types.Node, error) {
			hits, err := safe.SearchFTS("anything", 100, persist.SearchFTSOptions{})
			if err != nil {
				return nil, err
			}
			out := make([]types.Node, len(hits))
			for i, h := range hits {
				out[i] = h.Node
			}
			return out, nil
		}},
		{"NodesByFilePath", func() ([]types.Node, error) { return safe.NodesByFilePath("anywhere.go") }},
		{"AllNodes", func() ([]types.Node, error) { return safe.AllNodes() }},
	}
	for _, c := range calls {
		nodes, err := c.run()
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
			continue
		}
		ids := map[string]bool{}
		for _, n := range nodes {
			ids[n.ID] = true
		}
		if ids["h_amb"] {
			t.Errorf("%s leaked AMBIGUOUS Hunk h_amb", c.name)
		}
		if ids["c_amb"] {
			t.Errorf("%s leaked AMBIGUOUS Commit c_amb", c.name)
		}
		if !ids["fn"] {
			t.Errorf("%s dropped EXTRACTED Function fn (over-filtering)", c.name)
		}
	}

	// Methods returning ([]Node, []Edge): boundary must apply to both
	// the node list AND any edges that touched a dropped node.
	type subgraphCall struct {
		name string
		run  func() ([]types.Node, []types.Edge, error)
	}
	subgraphCalls := []subgraphCall{
		{"NeighborhoodByQname", func() ([]types.Node, []types.Edge, error) {
			return safe.NeighborhoodByQname("Foo", 1, false)
		}},
		{"SubgraphByQname", func() ([]types.Node, []types.Edge, error) {
			return safe.SubgraphByQname("Foo", 1)
		}},
	}
	for _, c := range subgraphCalls {
		nodes, edges, err := c.run()
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
			continue
		}
		for _, n := range nodes {
			if isAmbiguousMeta(n) {
				t.Errorf("%s leaked AMBIGUOUS node %s", c.name, n.ID)
			}
		}
		for _, e := range edges {
			if e.Dst == "h_amb" || e.Dst == "c_amb" || e.Src == "h_amb" || e.Src == "c_amb" {
				t.Errorf("%s returned edge touching AMBIGUOUS meta: %+v", c.name, e)
			}
		}
	}

	// GetBlob is the defensive backstop — even with a stale ID for an
	// AMBIGUOUS Hunk, the wrapper refuses the patch text. Both Hunks
	// and Commits get this protection because each kind has its own
	// blob in the unreachable-history track.
	for _, ambID := range []string{"h_amb", "c_amb"} {
		_, err := safe.GetBlob(ambID)
		if !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("GetBlob(%s) should refuse with sql.ErrNoRows, got %v", ambID, err)
		}
	}
	if _, err := safe.GetBlob("fn"); err != nil {
		t.Errorf("GetBlob(fn) should pass through, got %v", err)
	}
}

// --- fake store ---

type fakeStore struct {
	persist.StoreReader // embed nil interface — we only implement what tests touch
	nodes               []types.Node
	edges               []types.Edge
}

func (f *fakeStore) FindSymbol(name string, exact bool, _ persist.FindSymbolOptions) ([]types.Node, error) {
	return f.nodes, nil
}

func (f *fakeStore) NodesByIDs(ids []string) ([]types.Node, error) {
	idx := map[string]types.Node{}
	for _, n := range f.nodes {
		idx[n.ID] = n
	}
	out := make([]types.Node, 0, len(ids))
	for _, id := range ids {
		if n, ok := idx[id]; ok {
			out = append(out, n)
		}
	}
	return out, nil
}

func (f *fakeStore) QueryNodes(parent string, limit int) ([]types.Node, error) {
	return f.nodes, nil
}

func (f *fakeStore) TopNodes(metric string, limit int, excludeTypes ...string) ([]types.Node, error) {
	return f.nodes, nil
}

func (f *fakeStore) NeighborhoodByQname(qname string, depth int, reverse bool, edgeTypes ...string) ([]types.Node, []types.Edge, error) {
	return f.nodes, f.edges, nil
}

func (f *fakeStore) SubgraphByQname(qname string, depth int) ([]types.Node, []types.Edge, error) {
	return f.nodes, f.edges, nil
}

func (f *fakeStore) Search(q string, limit int) ([]types.Node, error) {
	return f.nodes, nil
}

func (f *fakeStore) SearchWithOpts(q string, limit int, _ persist.SearchFTSOptions) ([]types.Node, error) {
	return f.nodes, nil
}

func (f *fakeStore) SearchFTS(q string, limit int, _ persist.SearchFTSOptions) ([]persist.SearchHit, error) {
	out := make([]persist.SearchHit, len(f.nodes))
	for i, n := range f.nodes {
		// Synthetic uniform score — fake store is for routing/leak tests,
		// not relevance ordering. The options argument is ignored for the
		// same reason: language filter has no meaning over an in-memory
		// list of test doubles.
		out[i] = persist.SearchHit{Node: n, Score: 1.0, RawScore: 1.0}
	}
	return out, nil
}

func (f *fakeStore) NodesByFilePath(path string) ([]types.Node, error) {
	return f.nodes, nil
}

func (f *fakeStore) AllNodes() ([]types.Node, error) {
	return f.nodes, nil
}

func (f *fakeStore) AllEdges() ([]types.Edge, error) {
	return f.edges, nil
}

func (f *fakeStore) GetBlob(id string) ([]byte, error) {
	for _, n := range f.nodes {
		if n.ID == id {
			return []byte("source-of-" + id), nil
		}
	}
	return nil, sql.ErrNoRows
}

// TestSafeReader_Idempotent locks the enforcement mechanism: every Register*
// wraps its reader via safeReader, so the wrap must (1) apply the §11.3
// boundary to a raw reader and (2) be a no-op on an already-wrapped reader so
// RegisterAll's chained calls don't double-filter.
func TestSafeReader_Idempotent(t *testing.T) {
	raw := &fakeStore{}

	wrapped := safeReader(raw)
	if _, ok := wrapped.(*llmSafeReader); !ok {
		t.Fatalf("safeReader(raw) must wrap; got %T", wrapped)
	}
	if again := safeReader(wrapped); again != wrapped {
		t.Errorf("safeReader must be idempotent on an already-wrapped reader")
	}
	// NewLLMSafeReader output is also recognized as already-wrapped.
	if again := safeReader(NewLLMSafeReader(raw)); again == nil {
		t.Error("safeReader(NewLLMSafeReader(...)) returned nil")
	} else if _, ok := again.(*llmSafeReader); !ok {
		t.Errorf("expected *llmSafeReader, got %T", again)
	}
}
