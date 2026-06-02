package persist_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// fixtureStoreWithPRs stands up a fresh on-disk SQLite store with a
// single node and three node_prs rows spanning the 2026-04 / 2026-05 /
// 2026-06 window — enough to exercise cutoff slicing (ckg-NEW-3) and
// the descending-order contract.
func fixtureStoreWithPRs(t *testing.T) persist.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "graph.db")
	store, err := persist.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	node := types.Node{
		ID: "abcdef0123456789", Type: types.NodeFunction,
		Name: "Foo", QualifiedName: "pkg.Foo",
		FilePath: "pkg/foo.go", StartLine: 1, EndLine: 5,
		StartByte: 0, EndByte: 100,
		Language: "go", Confidence: types.ConfExtracted,
	}
	if err := store.InsertNodes([]types.Node{node}); err != nil {
		t.Fatalf("InsertNodes: %v", err)
	}
	mustParse := func(ts string) time.Time {
		t.Helper()
		v, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			t.Fatalf("parse %q: %v", ts, err)
		}
		return v.UTC()
	}
	refs := []types.PRRef{
		{Number: 42, Title: "Apr PR", MergedAtUTC: mustParse("2026-04-15T10:00:00Z"),
			Repo: "owner/name", BaseSHA: "aaa", HeadSHA: "bbb"},
		{Number: 43, Title: "May PR", MergedAtUTC: mustParse("2026-05-15T10:00:00Z"),
			Repo: "owner/name"},
		{Number: 44, Title: "Jun PR", MergedAtUTC: mustParse("2026-06-15T10:00:00Z"),
			Repo: "owner/name"},
	}
	if err := store.InsertNodePRs(map[string][]types.PRRef{node.ID: refs}); err != nil {
		t.Fatalf("InsertNodePRs: %v", err)
	}
	return store
}

// TestGetNodePRs_AllAndDescendingOrder confirms the zero-value cutoff
// surfaces every recorded PR and the descending-by-merged_at contract
// holds. Pagination consumers depend on the stable order.
func TestGetNodePRs_AllAndDescendingOrder(t *testing.T) {
	store := fixtureStoreWithPRs(t)
	got, err := store.GetNodePRs("abcdef0123456789", time.Time{})
	if err != nil {
		t.Fatalf("GetNodePRs: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 PRs, got %d", len(got))
	}
	if got[0].Number != 44 || got[1].Number != 43 || got[2].Number != 42 {
		t.Errorf("order: want [#44, #43, #42], got [%d, %d, %d]",
			got[0].Number, got[1].Number, got[2].Number)
	}
	// Round-trip fidelity on the optional fields.
	if got[2].BaseSHA != "aaa" || got[2].HeadSHA != "bbb" {
		t.Errorf("base/head SHA: got %q/%q", got[2].BaseSHA, got[2].HeadSHA)
	}
	if got[0].Repo != "owner/name" {
		t.Errorf("repo: got %q", got[0].Repo)
	}
}

// TestGetNodePRs_CutoffStrictBefore documents the ckg-NEW-3 contract:
// `WHERE merged_at < ?` — strictly before. The May PR (merged on
// 2026-05-15) is hidden when cutoff equals or precedes that
// timestamp, so a cks scenario answering "what did the agent know
// at 2026-05-15T10:00:00Z?" never sees the same-second PR.
func TestGetNodePRs_CutoffStrictBefore(t *testing.T) {
	store := fixtureStoreWithPRs(t)

	mustParse := func(ts string) time.Time {
		t.Helper()
		v, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			t.Fatalf("parse %q: %v", ts, err)
		}
		return v
	}

	// Cutoff strictly before May → only the April PR survives.
	got, err := store.GetNodePRs("abcdef0123456789", mustParse("2026-05-15T10:00:00Z"))
	if err != nil {
		t.Fatalf("GetNodePRs cutoff May: %v", err)
	}
	if len(got) != 1 || got[0].Number != 42 {
		t.Errorf("cutoff May-15: want [#42], got %d entries", len(got))
	}

	// Cutoff after every PR → all three.
	got, err = store.GetNodePRs("abcdef0123456789", mustParse("2027-01-01T00:00:00Z"))
	if err != nil {
		t.Fatalf("GetNodePRs cutoff Jan 2027: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("cutoff Jan 2027: want 3, got %d", len(got))
	}

	// Cutoff before April → empty.
	got, err = store.GetNodePRs("abcdef0123456789", mustParse("2026-01-01T00:00:00Z"))
	if err != nil {
		t.Fatalf("GetNodePRs cutoff Jan 2026: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("cutoff Jan 2026: want empty, got %d", len(got))
	}
}

// TestGetNodePRs_UnknownNode confirms the empty-slice (not error)
// contract for nodes the breadcrumb scan never touched.
func TestGetNodePRs_UnknownNode(t *testing.T) {
	store := fixtureStoreWithPRs(t)
	got, err := store.GetNodePRs("0000000000000000", time.Time{})
	if err != nil {
		t.Errorf("unknown node id: unexpected error %v", err)
	}
	if len(got) != 0 {
		t.Errorf("unknown node id: want empty, got %d", len(got))
	}
}

// TestInsertNodePRs_Empty documents the no-op-on-empty contract —
// callers shouldn't open a transaction for nothing.
func TestInsertNodePRs_Empty(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "graph.db")
	store, err := persist.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := store.InsertNodePRs(nil); err != nil {
		t.Errorf("InsertNodePRs(nil): unexpected error %v", err)
	}
	if err := store.InsertNodePRs(map[string][]types.PRRef{}); err != nil {
		t.Errorf("InsertNodePRs(empty map): unexpected error %v", err)
	}
}
