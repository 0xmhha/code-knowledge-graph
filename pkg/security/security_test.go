package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

const validYAML = `
security_patterns:
  - id: "reentrancy.external_call_after_state_change"
    name: "Reentrancy: external call after state change"
    category: "reentrancy"
    severity: "high"
    description: "Calling an external contract before settling internal state lets the callee re-enter and observe stale balances."
    remediation: "Apply the checks-effects-interactions pattern: update state before the external call, or use a reentrancy guard."
    matches:
      - "Vault.withdraw"
      - "Token.transfer"
  - id: "access-control.unprotected-initializer"
    name: "Unprotected initializer"
    category: "access-control"
    severity: "critical"
    description: "An initializer that can be re-run lets anyone re-initialise critical state."
    matches:
      - "UUPSProxy.initialize"
`

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "security.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return p
}

// TestLoadFromFile_Happy round-trips a valid two-entry YAML and
// confirms every field survives parsing.
func TestLoadFromFile_Happy(t *testing.T) {
	f, err := LoadFromFile(writeTemp(t, validYAML))
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if len(f.SecurityPatterns) != 2 {
		t.Fatalf("want 2 entries, got %d", len(f.SecurityPatterns))
	}
	got := f.SecurityPatterns[0]
	if got.ID != "reentrancy.external_call_after_state_change" {
		t.Errorf("ID: got %q", got.ID)
	}
	if got.Severity != SeverityHigh {
		t.Errorf("Severity: got %q", got.Severity)
	}
	if len(got.Matches) != 2 {
		t.Errorf("Matches: got %d", len(got.Matches))
	}
	if got.Remediation == "" {
		t.Errorf("Remediation should be populated, got empty")
	}
}

// TestLoadFromFile_MissingFile wraps the os.ReadFile error.
func TestLoadFromFile_MissingFile(t *testing.T) {
	_, err := LoadFromFile("/nonexistent/security.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// TestLoadFromFile_MalformedYAML covers the parse error path.
func TestLoadFromFile_MalformedYAML(t *testing.T) {
	_, err := LoadFromFile(writeTemp(t, "security_patterns: not a list\n  bad indent"))
	if err == nil {
		t.Fatal("expected parse error for malformed yaml")
	}
}

// TestLoadFromFile_EmptyIDRejected — the SecurityPattern node uses
// id as QualifiedName; an empty id would collide silently.
func TestLoadFromFile_EmptyIDRejected(t *testing.T) {
	_, err := LoadFromFile(writeTemp(t, `security_patterns:
  - id: ""
    name: "Anonymous"
`))
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

// TestLoadFromFile_DuplicateIDRejected — INSERT OR REPLACE on the
// nodes PK would otherwise drop one silently.
func TestLoadFromFile_DuplicateIDRejected(t *testing.T) {
	_, err := LoadFromFile(writeTemp(t, `security_patterns:
  - id: "dup"
    name: "First"
  - id: "dup"
    name: "Second"
`))
	if err == nil {
		t.Fatal("expected error for duplicate id")
	}
}

// TestLoadFromFile_InvalidSeverityRejected — loose strings would let
// typos like "hgh" sneak past the boundary and surface as silent
// under-counts in downstream "severity ≥ high" filters.
func TestLoadFromFile_InvalidSeverityRejected(t *testing.T) {
	_, err := LoadFromFile(writeTemp(t, `security_patterns:
  - id: "bad.severity"
    name: "Typo"
    severity: "hgh"
    matches: []
`))
	if err == nil {
		t.Fatal("expected error for invalid severity")
	}
	if !strings.Contains(err.Error(), "invalid severity") {
		t.Errorf("error should mention invalid severity, got: %v", err)
	}
}

// TestLoadFromFile_EmptySeverityAllowed — operators may leave severity
// off when the pattern is informational or the rating is pending; this
// is distinct from invalid-severity rejection above.
func TestLoadFromFile_EmptySeverityAllowed(t *testing.T) {
	_, err := LoadFromFile(writeTemp(t, `security_patterns:
  - id: "info.pending"
    name: "Pending severity"
`))
	if err != nil {
		t.Errorf("empty severity should be allowed, got: %v", err)
	}
}

// TestResolve_MatchAndWarn exercises the matching loop: a known
// qname becomes a has_security_pattern edge; an unknown one surfaces
// as a ResolveWarning.
func TestResolve_MatchAndWarn(t *testing.T) {
	f := &File{SecurityPatterns: []Entry{
		{
			ID: "reentrancy.eg", Name: "Reentrancy example",
			Category: "reentrancy", Severity: SeverityHigh,
			Matches: []string{
				"Vault.withdraw",    // matches
				"not.a.real.symbol", // warning
			},
		},
	}}
	code := []types.Node{
		{ID: "n_vault_withdraw", QualifiedName: "Vault.withdraw",
			Type: types.NodeMethod, Name: "withdraw"},
	}
	res := Resolve(f, code, "security.yaml")
	if len(res.Nodes) != 1 {
		t.Fatalf("want 1 SecurityPattern node, got %d", len(res.Nodes))
	}
	if res.Nodes[0].Type != types.NodeSecurityPattern {
		t.Errorf("node type: got %q", res.Nodes[0].Type)
	}
	if res.Nodes[0].SubKind != "reentrancy" {
		t.Errorf("node SubKind: got %q", res.Nodes[0].SubKind)
	}
	if !strings.Contains(res.Nodes[0].Signature, "severity=high") {
		t.Errorf("Signature should carry severity, got %q", res.Nodes[0].Signature)
	}
	if len(res.Edges) != 1 {
		t.Fatalf("want 1 has_security_pattern edge, got %d", len(res.Edges))
	}
	if res.Edges[0].Type != types.EdgeHasSecurityPattern {
		t.Errorf("edge type: got %q", res.Edges[0].Type)
	}
	if res.Edges[0].Src != "n_vault_withdraw" {
		t.Errorf("edge src: got %q", res.Edges[0].Src)
	}
	if res.Edges[0].Dst != "security:reentrancy.eg" {
		t.Errorf("edge dst: got %q", res.Edges[0].Dst)
	}
	if len(res.Warnings) != 1 {
		t.Fatalf("want 1 warning, got %d", len(res.Warnings))
	}
}

// TestResolve_EmptyInputs locks the nil-safe paths.
func TestResolve_EmptyInputs(t *testing.T) {
	res := Resolve(nil, nil, "")
	if len(res.Nodes) != 0 || len(res.Edges) != 0 {
		t.Errorf("nil File should yield empty result: %+v", res)
	}
	res = Resolve(&File{}, nil, "")
	if len(res.Nodes) != 0 {
		t.Errorf("empty File should yield empty result: %+v", res)
	}
}

// TestResolve_DocCommentComposition exercises the description +
// remediation fold: both, only description, only remediation,
// neither.
func TestResolve_DocCommentComposition(t *testing.T) {
	cases := []struct {
		name string
		e    Entry
		want string
	}{
		{"both", Entry{ID: "a", Description: "Risk.", Remediation: "Fix."},
			"Risk.\n\nRemediation: Fix."},
		{"description only", Entry{ID: "b", Description: "Risk."}, "Risk."},
		{"remediation only", Entry{ID: "c", Remediation: "Fix."},
			"Remediation: Fix."},
		{"neither", Entry{ID: "d"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := Resolve(&File{SecurityPatterns: []Entry{tc.e}}, nil, "")
			if len(res.Nodes) != 1 {
				t.Fatalf("want 1 node, got %d", len(res.Nodes))
			}
			if res.Nodes[0].DocComment != tc.want {
				t.Errorf("DocComment:\n got  %q\n want %q",
					res.Nodes[0].DocComment, tc.want)
			}
		})
	}
}
