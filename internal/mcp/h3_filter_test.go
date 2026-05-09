package mcp

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
	safe := newLLMSafeStoreReader(fake)

	// FindSymbol — exercise the filtered read path.
	out, err := safe.FindSymbol("", "", true)
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

// --- fake store ---

type fakeStore struct {
	persist.StoreReader // embed nil interface — we only implement what tests touch
	nodes               []types.Node
	edges               []types.Edge
}

func (f *fakeStore) FindSymbol(name, lang string, exact bool) ([]types.Node, error) {
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

func (f *fakeStore) SubgraphByQname(qname string, depth int) ([]types.Node, []types.Edge, error) {
	return f.nodes, f.edges, nil
}

func (f *fakeStore) GetBlob(id string) ([]byte, error) {
	for _, n := range f.nodes {
		if n.ID == id {
			return []byte("source-of-" + id), nil
		}
	}
	return nil, sql.ErrNoRows
}
