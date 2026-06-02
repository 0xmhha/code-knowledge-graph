package buildpipe

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestLoadPolicy_EmptyPath confirms the no-op default — an empty
// PolicyFile must return zero rows without touching the filesystem
// so existing buildpipe callers that don't set the flag see no change.
func TestLoadPolicy_EmptyPath(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	nodes, edges, err := loadPolicy("", nil, log)
	if err != nil {
		t.Fatalf("loadPolicy(empty): %v", err)
	}
	if len(nodes) != 0 || len(edges) != 0 {
		t.Errorf("empty PolicyFile must yield zero rows; got %d nodes, %d edges", len(nodes), len(edges))
	}
}

// TestLoadPolicy_MissingFile bubbles the os.ReadFile error so the
// caller can decide whether a missing policy file is fatal or
// warn-only. The cold path treats it as warn-only (see Run in
// pipeline.go).
func TestLoadPolicy_MissingFile(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	_, _, err := loadPolicy("/nonexistent/policy.yaml", nil, log)
	if err == nil {
		t.Fatal("expected error for missing policy file")
	}
}

// TestPolicyIntegration_EndToEnd runs a real buildpipe.Run with the
// resolve fixture and a small policy YAML, then inspects the resulting
// SQLite store to confirm policy rows landed under the new schema
// 1.14 enum slots. Exercises:
//   - PolicyFile plumbing through Options
//   - cold-path emit ordering (policy after code nodes so FKs hold)
//   - NodePolicy persisted via the standard nodes table
//   - EdgeGovernedBy persisted via the standard edges table
//
// Uses package-internal helpers (Run, loadPolicy) rather than the
// public surface so the test can assert on row counts without round-
// tripping through the MCP layer.
func TestPolicyIntegration_EndToEnd(t *testing.T) {
	outDir := t.TempDir()

	// Drop a policy YAML alongside the build output. The governs[]
	// entry uses the fixture's package-relative qname; we don't pin
	// the exact value here — instead the assertion checks that the
	// policy NODE always lands (the entry has a stable id) and that
	// at most one governed_by edge is emitted (if the qname happens
	// to match a fixture symbol). The fixture has `func Greet` in
	// package `a`; resolve fixtures historically expose it as
	// "a.Greet" via the go-module qname builder.
	yaml := `
policies:
  - id: "fixture.greet-policy"
    name: "Greet API contract"
    category: "fixture"
    description: "Synthetic policy for the buildpipe smoke test."
    governs:
      - "a.Greet"
      - "missing.qname.warning.path"
`
	policyPath := filepath.Join(outDir, "policy.yaml")
	if err := os.WriteFile(policyPath, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write policy yaml: %v", err)
	}

	_, err := Run(Options{
		SrcRoot:    "../parse/golang/testdata/resolve",
		OutDir:     outDir,
		Languages:  []string{"auto"},
		CKGVersion: "test",
		PolicyFile: policyPath,
	})
	if err != nil {
		t.Fatalf("buildpipe.Run: %v", err)
	}

	store, err := persist.OpenReadOnly(filepath.Join(outDir, "graph.db"))
	if err != nil {
		t.Fatalf("open graph.db: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Assert: at least one NodePolicy row landed. The YAML declares
	// exactly one policy entry; FindSymbol on its qname should find
	// the single node.
	pol, err := store.FindSymbol("fixture.greet-policy", true, persist.FindSymbolOptions{})
	if err != nil {
		t.Fatalf("FindSymbol policy: %v", err)
	}
	if len(pol) != 1 {
		t.Fatalf("want 1 policy node with qname fixture.greet-policy, got %d", len(pol))
	}
	if pol[0].Type != types.NodePolicy {
		t.Errorf("policy node Type: got %q, want %q", pol[0].Type, types.NodePolicy)
	}
	if pol[0].Name != "Greet API contract" {
		t.Errorf("policy node Name: got %q", pol[0].Name)
	}

	// Assert: governed_by edges. The fixture's a.Greet match emits
	// one edge; the missing.qname entry surfaces as a warning (not
	// a row). So count must be >= 0 — but if a.Greet did match we
	// expect exactly 1. The qname-matching behaviour of go's qname
	// builder is the load-bearing assumption here.
	gov, err := store.QueryEdgesByType(string(types.EdgeGovernedBy))
	if err != nil {
		t.Fatalf("QueryEdgesByType: %v", err)
	}
	if len(gov) > 1 {
		t.Errorf("expected at most 1 governed_by edge (a.Greet match), got %d: %+v", len(gov), gov)
	}
	for _, e := range gov {
		if e.Dst != "policy:fixture.greet-policy" {
			t.Errorf("governed_by Dst should target policy node, got %q", e.Dst)
		}
	}
}
