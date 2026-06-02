package buildpipe

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestGoStablenetPolicySecurityBuild measures M2.d on REAL go-stablenet code:
// a build with policy + security YAML that governs actual go-stablenet symbols
// must populate NodePolicy / NodeSecurityPattern and the governed_by /
// has_security_pattern edges. The synthetic end-to-end is covered by
// TestPolicyIntegration_EndToEnd + the security_load test; this proves the
// resolver also matches go-stablenet's real qname format (e.g. core.NewBlockChain).
//
// Opt-in via CKG_GSN_SRC (the go-stablenet source root); slow (full Go build).
// The policy/security YAML here is a minimal STARTER — 03-cks's codegen will
// later derive the real files from the cks domain entries.
func TestGoStablenetPolicySecurityBuild(t *testing.T) {
	src := os.Getenv("CKG_GSN_SRC")
	if src == "" {
		t.Skip("set CKG_GSN_SRC=<go-stablenet source root> to run the go-stablenet policy/security 실측")
	}
	out := t.TempDir()

	const policyYAML = `
policies:
  - id: "gsn.blockchain-init"
    name: "BlockChain construction is consensus-critical"
    category: "consensus"
    description: "Changes to chain construction affect genesis and fork choice."
    governs:
      - "core.NewBlockChain"
`
	const securityYAML = `
security_patterns:
  - id: "gsn.chain-insert-validation"
    name: "Block insertion must validate"
    category: "consensus"
    severity: "high"
    description: "InsertChain must not bypass block validation."
    matches:
      - "core.BlockChain.InsertChain"
`
	pPath := filepath.Join(out, "policy.yaml")
	sPath := filepath.Join(out, "security.yaml")
	if err := os.WriteFile(pPath, []byte(policyYAML), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	if err := os.WriteFile(sPath, []byte(securityYAML), 0o644); err != nil {
		t.Fatalf("write security: %v", err)
	}

	if _, err := Run(Options{
		SrcRoot:             src,
		OutDir:              out,
		Languages:           []string{"go"}, // Go-only keeps the build focused/faster
		CKGVersion:          "test",
		PolicyFile:          pPath,
		SecurityPatternFile: sPath,
	}); err != nil {
		t.Fatalf("buildpipe.Run on go-stablenet: %v", err)
	}

	st, err := persist.OpenReadOnly(filepath.Join(out, "graph.db"))
	if err != nil {
		t.Fatalf("open graph.db: %v", err)
	}
	defer func() { _ = st.Close() }()

	pol, err := st.FindSymbol("gsn.blockchain-init", true, persist.FindSymbolOptions{})
	if err != nil {
		t.Fatalf("FindSymbol policy: %v", err)
	}
	if len(pol) != 1 || pol[0].Type != types.NodePolicy {
		t.Errorf("want 1 NodePolicy (gsn.blockchain-init), got %d", len(pol))
	}
	sec, err := st.FindSymbol("gsn.chain-insert-validation", true, persist.FindSymbolOptions{})
	if err != nil {
		t.Fatalf("FindSymbol security: %v", err)
	}
	if len(sec) != 1 || sec[0].Type != types.NodeSecurityPattern {
		t.Errorf("want 1 NodeSecurityPattern (gsn.chain-insert-validation), got %d", len(sec))
	}

	// The resolver must have matched the go-stablenet symbols and emitted edges.
	gov, err := st.QueryEdgesByType(string(types.EdgeGovernedBy))
	if err != nil {
		t.Fatalf("QueryEdgesByType(governed_by): %v", err)
	}
	hsp, err := st.QueryEdgesByType(string(types.EdgeHasSecurityPattern))
	if err != nil {
		t.Fatalf("QueryEdgesByType(has_security_pattern): %v", err)
	}
	if len(gov) == 0 {
		t.Error("want >=1 governed_by edge to core.NewBlockChain (resolver matched real go-stablenet qname)")
	}
	if len(hsp) == 0 {
		t.Error("want >=1 has_security_pattern edge to core.BlockChain.InsertChain")
	}
	t.Logf("M2.d go-stablenet: NodePolicy=%d NodeSecurityPattern=%d governed_by=%d has_security_pattern=%d",
		len(pol), len(sec), len(gov), len(hsp))
}
