package eval

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	pkgstore "github.com/0xmhha/code-knowledge-graph/pkg/store"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// fakeStore is a minimal in-package double for pkgstore.Reader,
// sufficient for the hallucination_check.go unit tests. Only
// FindSymbol carries real behaviour — every other method returns
// zero values. The same intra-module pattern runner_internal_test.go
// uses (imports internal/persist directly because it lives under the
// same module root) avoids the heavier setup of building a real
// SQLite graph for every unit test.
type fakeStore struct {
	byName map[string][]types.Node
}

func (f *fakeStore) Close() error                                           { return nil }
func (f *fakeStore) GetManifest() (persist.Manifest, error)                 { return persist.Manifest{}, nil }
func (f *fakeStore) LoadHierarchy(_ string) ([]persist.HierarchyRow, error) { return nil, nil }
func (f *fakeStore) FindSymbol(name string, exact bool, _ pkgstore.FindSymbolOptions) ([]types.Node, error) {
	if exact {
		return f.byName[name], nil
	}
	// V0: case-insensitive sweep iterates the byName map. Adequate
	// for ~10-symbol test fixtures.
	var out []types.Node
	low := lower(name)
	for k, nodes := range f.byName {
		if lower(k) == low {
			out = append(out, nodes...)
		}
	}
	return out, nil
}
func (f *fakeStore) NodesByIDs(_ []string) ([]types.Node, error)      { return nil, nil }
func (f *fakeStore) QueryNodes(_ string, _ int) ([]types.Node, error) { return nil, nil }
func (f *fakeStore) TopNodes(_ string, _ int, _ ...string) ([]types.Node, error) {
	return nil, nil
}
func (f *fakeStore) DistinctFilePaths(_ string) ([]string, error)        { return nil, nil }
func (f *fakeStore) QueryEdgesByType(_ string) ([]types.Edge, error)     { return nil, nil }
func (f *fakeStore) QueryEdgesForNodes(_ []string) ([]types.Edge, error) { return nil, nil }
func (f *fakeStore) EdgeCountsByType() (map[string]int, error)           { return nil, nil }
func (f *fakeStore) NeighborhoodByQname(_ string, _ int, _ bool, _ ...string) ([]types.Node, []types.Edge, error) {
	return nil, nil, nil
}
func (f *fakeStore) SubgraphByQname(_ string, _ int) ([]types.Node, []types.Edge, error) {
	return nil, nil, nil
}
func (f *fakeStore) Search(_ string, _ int) ([]types.Node, error) { return nil, nil }
func (f *fakeStore) SearchFTS(_ string, _ int, _ pkgstore.SearchFTSOptions) ([]pkgstore.SearchHit, error) {
	return nil, nil
}
func (f *fakeStore) GetBlob(_ string) ([]byte, error)                    { return nil, nil }
func (f *fakeStore) NodesByFilePath(_ string) ([]types.Node, error)      { return nil, nil }
func (f *fakeStore) EdgesByFilePath(_ string) ([]types.Edge, error)      { return nil, nil }
func (f *fakeStore) BlobsByFilePath(_ string) (map[string][]byte, error) { return nil, nil }
func (f *fakeStore) PendingRefsByFilePath(_ string) ([]persist.PendingRefRow, error) {
	return nil, nil
}
func (f *fakeStore) ReverseDepsForFiles(_ []string) ([]string, error) { return nil, nil }
func (f *fakeStore) ExportChunked(_ string, _, _ int) error           { return nil }
func (f *fakeStore) AmbiguousMetaNodes() ([]types.Node, error)        { return nil, nil }
func (f *fakeStore) AllNodes() ([]types.Node, error)                  { return nil, nil }
func (f *fakeStore) AllEdges() ([]types.Edge, error)                  { return nil, nil }

func lower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func mkStore(nodes ...types.Node) *fakeStore {
	by := map[string][]types.Node{}
	for _, n := range nodes {
		by[n.Name] = append(by[n.Name], n)
	}
	return &fakeStore{byName: by}
}

// TestValidateMentions_AllFound — happy path. Every mention resolves
// with a qname-exact match; Hallucinated is empty, Rate=0,
// QnameDiverged is empty.
func TestValidateMentions_AllFound(t *testing.T) {
	store := mkStore(
		types.Node{Name: "NewBlockChain", QualifiedName: "core.NewBlockChain"},
		types.Node{Name: "Deposit", QualifiedName: "service.Vault.Deposit"},
	)
	out := "Call `core.NewBlockChain` followed by service.Vault.Deposit."
	got, err := ValidateMentions(out, store)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Total != 2 {
		t.Errorf("Total: got %d, want 2 (mentions=%v)", got.Total, got)
	}
	if len(got.Hallucinated) != 0 {
		t.Errorf("Hallucinated: got %v, want empty", got.Hallucinated)
	}
	if len(got.QnameDiverged) != 0 {
		t.Errorf("QnameDiverged: got %v, want empty", got.QnameDiverged)
	}
	if got.Rate != 0 {
		t.Errorf("Rate: got %v, want 0", got.Rate)
	}
}

// TestValidateMentions_Hallucinated — the LLM mentions a symbol that
// doesn't exist in the store. The bare name fails both exact and
// case-insensitive lookups, so it lands in Hallucinated.
func TestValidateMentions_Hallucinated(t *testing.T) {
	store := mkStore(
		types.Node{Name: "NewBlockChain", QualifiedName: "core.NewBlockChain"},
	)
	out := "Use `core.NewBlockChain` and then `core.FabricateWidget` to wire it up."
	got, err := ValidateMentions(out, store)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Total != 2 {
		t.Fatalf("Total: got %d, want 2", got.Total)
	}
	wantHallu := []string{"core.FabricateWidget"}
	if !sameSet(got.Hallucinated, wantHallu) {
		t.Errorf("Hallucinated: got %v, want %v", got.Hallucinated, wantHallu)
	}
	if got.Rate != 0.5 {
		t.Errorf("Rate: got %v, want 0.5", got.Rate)
	}
}

// TestValidateMentions_QnameSuffix_NotDiverged — V2 (2026-05-21,
// T-04 second smoke run): a mention that is a segment-aware
// case-insensitive suffix of the qname counts as a clean qname
// match, NOT as a divergence. Locks the short-qname pattern
// (LLM writes "Vault.deposit" for store qname
// "service.Vault.Deposit") that V1 flagged falsely.
func TestValidateMentions_QnameSuffix_NotDiverged(t *testing.T) {
	store := mkStore(
		types.Node{Name: "Deposit", QualifiedName: "service.Vault.Deposit"},
	)
	// Two mentions that case-fold to the same key — dedup leaves 1.
	out := "Look at Vault.Deposit and also vault.deposit (case variants)."
	got, err := ValidateMentions(out, store)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Total != 1 {
		t.Fatalf("Total: got %d, want 1 (case-folded dedup collapses Vault.Deposit and vault.deposit)", got.Total)
	}
	if len(got.Hallucinated) != 0 {
		t.Errorf("Hallucinated: got %v, want empty", got.Hallucinated)
	}
	if len(got.QnameDiverged) != 0 {
		t.Errorf("QnameDiverged: got %v, want empty (V2 suffix match should clear short-qname mentions)", got.QnameDiverged)
	}
}

// TestValidateMentions_SingleSegmentSuffix — bare name `Deposit`
// alone matches qname `service.Vault.Deposit` via 1-segment
// suffix. V2 covers this trivially (mSegs=[Deposit], qSegs=[...,
// Deposit] aligns at the last segment).
func TestValidateMentions_SingleSegmentSuffix(t *testing.T) {
	store := mkStore(
		types.Node{Name: "Deposit", QualifiedName: "service.Vault.Deposit"},
	)
	// "Deposit" alone won't match extractSymbols' "must contain a dot"
	// filter, so this test exercises the suffix-match path through a
	// 2-segment mention that aligns on the last segment only.
	out := "Use Helper.Deposit somewhere unrelated."
	got, err := ValidateMentions(out, store)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Total != 1 {
		t.Fatalf("Total: got %d, want 1", got.Total)
	}
	// `Helper.Deposit` is NOT a suffix of `service.Vault.Deposit` —
	// `Helper` ≠ `Vault`. The bare name `Deposit` resolves, but the
	// qname does not align, so this lands in QnameDiverged as
	// expected.
	if len(got.QnameDiverged) != 1 {
		t.Errorf("QnameDiverged: got %v, want [Helper.Deposit] (segment misalignment at index -2)", got.QnameDiverged)
	}
}

// TestValidateMentions_ReceiverStyle_StillDiverged — V2 explicitly
// does NOT cover receiver-style mentions like `h.vault.Deposit`
// where `h` is a local variable. The leading variable segment
// defeats segment-aligned suffix match, so the mention still
// surfaces in QnameDiverged for V3 design (first-segment-variable
// heuristic).
func TestValidateMentions_ReceiverStyle_StillDiverged(t *testing.T) {
	store := mkStore(
		types.Node{Name: "Deposit", QualifiedName: "service.Vault.Deposit"},
	)
	out := "h.vault.Deposit is the call site."
	got, err := ValidateMentions(out, store)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Total != 1 {
		t.Fatalf("Total: got %d, want 1", got.Total)
	}
	if len(got.Hallucinated) != 0 {
		t.Errorf("Hallucinated: got %v, want empty (bare name resolves)", got.Hallucinated)
	}
	if !sameSet(got.QnameDiverged, []string{"h.vault.Deposit"}) {
		t.Errorf("QnameDiverged: got %v, want [h.vault.Deposit] (V3 territory)", got.QnameDiverged)
	}
}

// TestValidateMentions_QnameDiverged — the bare name resolves but
// against a different qualified name. Found includes the mention,
// QnameDiverged also includes it, Hallucinated does not.
//
// This is the stablenet pattern: real symbol is core.NewBlockChain,
// LLM says eth.NewBlockChain. The bare 'NewBlockChain' resolves, but
// the dotted prefix is wrong. V0 surfaces this for triage but does
// not count it as hallucination.
func TestValidateMentions_QnameDiverged(t *testing.T) {
	store := mkStore(
		types.Node{Name: "NewBlockChain", QualifiedName: "core.NewBlockChain"},
	)
	out := "Look at eth.NewBlockChain in the bootstrap path."
	got, err := ValidateMentions(out, store)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Total != 1 {
		t.Fatalf("Total: got %d, want 1", got.Total)
	}
	if len(got.Hallucinated) != 0 {
		t.Errorf("Hallucinated: got %v, want empty (bare name resolves)", got.Hallucinated)
	}
	if !sameSet(got.Found, []string{"eth.NewBlockChain"}) {
		t.Errorf("Found: got %v, want [eth.NewBlockChain]", got.Found)
	}
	if !sameSet(got.QnameDiverged, []string{"eth.NewBlockChain"}) {
		t.Errorf("QnameDiverged: got %v, want [eth.NewBlockChain]", got.QnameDiverged)
	}
	if got.Rate != 0 {
		t.Errorf("Rate: got %v, want 0 (qname divergence does not count against rate)", got.Rate)
	}
}

// TestValidateMentions_CaseInsensitiveSweep — the LLM lowercases the
// type in prose ("vault.deposit" instead of "Vault.Deposit"). The
// exact-true lookup misses but the case-insensitive sweep hits, so
// the mention lands in Found.
func TestValidateMentions_CaseInsensitiveSweep(t *testing.T) {
	store := mkStore(
		types.Node{Name: "Deposit", QualifiedName: "service.Vault.Deposit"},
	)
	out := "The deposit path is service.vault.deposit."
	got, err := ValidateMentions(out, store)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Total != 1 {
		t.Fatalf("Total: got %d, want 1 (got=%+v)", got.Total, got)
	}
	if len(got.Hallucinated) != 0 {
		t.Errorf("Hallucinated: got %v, want empty (CI sweep should hit)", got.Hallucinated)
	}
}

// TestValidateMentions_NilStore — call sites without a graph (rubric-
// only scoring) pass nil and get every mention as Found, Rate=0.
func TestValidateMentions_NilStore(t *testing.T) {
	out := "core.NewBlockChain and core.FabricateWidget."
	got, err := ValidateMentions(out, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Total != 2 {
		t.Errorf("Total: got %d, want 2", got.Total)
	}
	if len(got.Found) != 2 {
		t.Errorf("Found: got %v, want both mentions", got.Found)
	}
	if got.Rate != 0 {
		t.Errorf("Rate: got %v, want 0", got.Rate)
	}
}

// TestValidateMentions_DedupCaseInsensitive — the same mention three
// times (once lowercased) counts once.
func TestValidateMentions_DedupCaseInsensitive(t *testing.T) {
	store := mkStore(
		types.Node{Name: "NewBlockChain", QualifiedName: "core.NewBlockChain"},
	)
	out := "core.NewBlockChain or core.newblockchain or `core.NewBlockChain` again."
	got, err := ValidateMentions(out, store)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Total != 1 {
		t.Errorf("Total: got %d, want 1 (dedupe collapses 3 mentions to 1)", got.Total)
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, x := range a {
		m[lower(x)]++
	}
	for _, y := range b {
		m[lower(y)]--
	}
	for _, v := range m {
		if v != 0 {
			return false
		}
	}
	return true
}
