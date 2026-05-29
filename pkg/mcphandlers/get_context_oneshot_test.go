package mcphandlers

import (
	"testing"
	"time"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/smartctx"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// prInjectingStore wraps a real persist.StoreReader and synthesises
// GetNodePRs results so the 1-shot retrieval tests can exercise the
// PR attachment branch without standing up a git-backed fixture.
// Every other method passes through to the embedded reader so the
// rest of BuildContext (Search, QueryEdgesForNodes, NodesByIDs,
// GetBlob, NeighborhoodByQname for impact) behaves exactly as in
// the production path.
//
// limit lets a test assert the PRsPerNode cap by returning more PRs
// than the cap allows.
type prInjectingStore struct {
	persist.StoreReader
	prByNode map[string][]types.PRRef
}

func (p *prInjectingStore) GetNodePRs(nodeID string, _ time.Time) ([]types.PRRef, error) {
	return p.prByNode[nodeID], nil
}

// TestBuildContext_PRsOptIn verifies the 1-shot retrieval contract:
// without IncludePRs the response carries no recent_prs key at all,
// matching the pre-P0 #2 shape that the eval δ baseline + existing
// MCP consumers rely on for cache stability.
func TestBuildContext_PRsOptIn(t *testing.T) {
	store := newFixtureStore(t)
	res, err := smartctx.BuildContext(store, "Greet", smartctx.Options{
		BudgetTokens: 4000, IncludeBlobs: true, MaxBodies: 5,
		IncludePRs: false,
	})
	if err != nil {
		t.Fatalf("BuildContext: %v", err)
	}
	if _, present := res["recent_prs"]; present {
		t.Error("recent_prs must be absent when IncludePRs=false")
	}
}

// TestBuildContext_PRsAttached fakes a PR breadcrumb for whatever
// node the retriever surfaces as a body and asserts that the
// recent_prs map carries an entry keyed by that node id.
func TestBuildContext_PRsAttached(t *testing.T) {
	base := newFixtureStore(t)

	// Run once with no PR injection to discover the body id the
	// retriever picks for the query — this keeps the test resilient
	// to changes in the fixture's symbol set.
	probe, err := smartctx.BuildContext(base, "Greet", smartctx.Options{
		BudgetTokens: 4000, IncludeBlobs: true, MaxBodies: 5,
	})
	if err != nil {
		t.Fatalf("probe BuildContext: %v", err)
	}
	bodies, _ := probe["bodies"].([]map[string]any)
	if len(bodies) == 0 {
		t.Skip("fixture produced no body entries for 'Greet'; nothing to attach PRs to")
	}
	targetID, _ := bodies[0]["id"].(string)
	if targetID == "" {
		t.Fatalf("body[0] missing id: %+v", bodies[0])
	}

	store := &prInjectingStore{
		StoreReader: base,
		prByNode: map[string][]types.PRRef{
			targetID: {
				{Number: 101, Title: "Add Greet", Summary: "Initial implementation."},
				{Number: 102, Title: "Refactor Greet", Summary: "Split into helpers."},
				{Number: 103, Title: "Fix Greet edge case", Summary: "Empty name path."},
				{Number: 104, Title: "Doc Greet", Summary: "Add usage examples."},
			},
		},
	}

	res, err := smartctx.BuildContext(store, "Greet", smartctx.Options{
		BudgetTokens: 4000, IncludeBlobs: true, MaxBodies: 5,
		IncludePRs: true, PRsPerNode: 3,
	})
	if err != nil {
		t.Fatalf("BuildContext with PRs: %v", err)
	}
	prs, ok := res["recent_prs"].(map[string][]types.PRRef)
	if !ok {
		t.Fatalf("recent_prs missing or wrong type: %T", res["recent_prs"])
	}
	got, present := prs[targetID]
	if !present {
		t.Fatalf("recent_prs has no entry for %s; got keys %v", targetID, mapStringKeys(prs))
	}
	if len(got) != 3 {
		t.Errorf("PRsPerNode cap not respected: got %d, want 3", len(got))
	}
	if got[0].Number != 101 {
		t.Errorf("first PR mismatch: got #%d, want #101", got[0].Number)
	}
}

// TestBuildContext_ImpactOptIn confirms the impact field is omitted
// by default and present (possibly with an error placeholder for a
// non-resolving qname) when IncludeImpact is on.
func TestBuildContext_ImpactOptIn(t *testing.T) {
	store := newFixtureStore(t)

	off, err := smartctx.BuildContext(store, "Greet", smartctx.Options{
		BudgetTokens: 4000, IncludeBlobs: false, MaxBodies: 5,
	})
	if err != nil {
		t.Fatalf("BuildContext off: %v", err)
	}
	if _, present := off["impact"]; present {
		t.Error("impact must be absent when IncludeImpact=false")
	}

	on, err := smartctx.BuildContext(store, "Greet", smartctx.Options{
		BudgetTokens: 4000, IncludeBlobs: false, MaxBodies: 5,
		IncludeImpact: true, ImpactDepth: 1,
	})
	if err != nil {
		t.Fatalf("BuildContext on: %v", err)
	}
	if notFound, _ := on["not_found"].(bool); notFound {
		t.Skip("fixture produced no hits for 'Greet'; impact branch not reachable")
	}
	if _, present := on["impact"]; !present {
		t.Error("impact must be present when IncludeImpact=true and rows!=nil")
	}
}

func mapStringKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
