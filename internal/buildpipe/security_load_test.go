package buildpipe

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestLoadSecurityPatterns_EmptyPath confirms the no-op default —
// an empty SecurityPatternFile must return zero rows so existing
// callers that don't set the flag see no change.
func TestLoadSecurityPatterns_EmptyPath(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	nodes, edges, err := loadSecurityPatterns("", nil, log)
	if err != nil {
		t.Fatalf("loadSecurityPatterns(empty): %v", err)
	}
	if len(nodes) != 0 || len(edges) != 0 {
		t.Errorf("empty SecurityPatternFile must yield zero rows; got %d nodes, %d edges",
			len(nodes), len(edges))
	}
}

// TestLoadSecurityPatterns_MissingFile bubbles the os.ReadFile error
// so the caller can decide whether a missing security file is fatal
// or warn-only. The cold path treats it as warn-only (see Run).
func TestLoadSecurityPatterns_MissingFile(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	_, _, err := loadSecurityPatterns("/nonexistent/security.yaml", nil, log)
	if err == nil {
		t.Fatal("expected error for missing security file")
	}
}

// TestSecurityIntegration_EndToEnd runs a real buildpipe.Run with the
// resolve fixture + a small security YAML, then inspects the SQLite
// store to confirm rows landed under the new schema 1.15 slots.
func TestSecurityIntegration_EndToEnd(t *testing.T) {
	outDir := t.TempDir()

	// One pattern matching the fixture's a.Greet, one that intentionally
	// misses to exercise the warning path. Severity = "high" is in the
	// closed enum so the loader's validation accepts it.
	yaml := `
security_patterns:
  - id: "fixture.greet-risk"
    name: "Greet exposes raw user input"
    category: "input-validation"
    severity: "medium"
    description: "Demonstration pattern for the buildpipe smoke test."
    remediation: "Validate and length-bound the name argument."
    matches:
      - "a.Greet"
      - "this.qname.is.intentionally.missing"
`
	yamlPath := filepath.Join(outDir, "security.yaml")
	if err := os.WriteFile(yamlPath, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write security yaml: %v", err)
	}

	_, err := Run(Options{
		SrcRoot:             "../parse/golang/testdata/resolve",
		OutDir:              outDir,
		Languages:           []string{"auto"},
		CKGVersion:          "test",
		SecurityPatternFile: yamlPath,
	})
	if err != nil {
		t.Fatalf("buildpipe.Run: %v", err)
	}

	store, err := persist.OpenReadOnly(filepath.Join(outDir, "graph.db"))
	if err != nil {
		t.Fatalf("open graph.db: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Exactly one SecurityPattern node landed.
	hits, err := store.FindSymbol("fixture.greet-risk", true, persist.FindSymbolOptions{})
	if err != nil {
		t.Fatalf("FindSymbol security: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("want 1 SecurityPattern node, got %d", len(hits))
	}
	if hits[0].Type != types.NodeSecurityPattern {
		t.Errorf("node Type: got %q, want %q", hits[0].Type, types.NodeSecurityPattern)
	}
	if hits[0].SubKind != "input-validation" {
		t.Errorf("SubKind: got %q", hits[0].SubKind)
	}
	if hits[0].Signature == "" {
		t.Errorf("Signature should carry severity tag, got empty")
	}

	// One has_security_pattern edge for the matched a.Greet target;
	// the missing entry surfaced as a log-level warning, not a row.
	edges, err := store.QueryEdgesByType(string(types.EdgeHasSecurityPattern))
	if err != nil {
		t.Fatalf("QueryEdgesByType: %v", err)
	}
	if len(edges) > 1 {
		t.Errorf("expected at most 1 has_security_pattern edge, got %d: %+v",
			len(edges), edges)
	}
	for _, e := range edges {
		if e.Dst != "security:fixture.greet-risk" {
			t.Errorf("edge Dst should target security node, got %q", e.Dst)
		}
	}
}
