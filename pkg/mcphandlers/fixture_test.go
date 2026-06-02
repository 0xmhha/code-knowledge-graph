package mcphandlers

// newFixtureStore is shared across test files that need a real
// persist.Store built from the resolve fixture. It runs buildpipe
// once per test into a temporary directory and registers Close via
// t.Cleanup.
//
// The file also carries the test-only shims (computeImpact /
// impactDepthCap) that the historical internal/mcp test suite uses.
// New tests should call pkg/impact.Compute / impact.DepthCap directly;
// these wrappers exist so the impact_test.go body (moved from
// internal/mcp during T-14b) keeps working without churn.

import (
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/impact"
)

func newFixtureStore(t *testing.T) persist.Store {
	t.Helper()
	out := t.TempDir()
	if _, err := buildpipe.Run(buildpipe.Options{
		SrcRoot:    "../../internal/parse/golang/testdata/resolve",
		OutDir:     out,
		Languages:  []string{"auto"},
		CKGVersion: "test",
	}); err != nil {
		t.Fatal(err)
	}
	store, err := persist.OpenReadOnly(filepath.Join(out, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// computeImpact is a thin pass-through retained so the migrated
// impact_test.go body keeps working with its historical 5-argument
// signature instead of having to switch to the impact.Options form
// in every call site.
func computeImpact(store persist.StoreReader, seedQname, seedFile string, depth int, includeBlobs bool) (map[string]any, error) {
	return impact.Compute(store, seedQname, seedFile, impact.Options{
		Depth:        depth,
		IncludeBlobs: includeBlobs,
	})
}

// impactDepthCap re-exports the shared cap so the existing
// TestImpact_DepthCap assertion (which compares against this constant)
// continues to work without referencing pkg/impact directly.
const impactDepthCap = impact.DepthCap
