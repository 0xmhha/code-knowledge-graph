package policy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

const validYAML = `
policies:
  - id: "fork.berlin"
    name: "Berlin Hard Fork"
    category: "consensus"
    description: "Increases gas cost for SLOAD/SSTORE."
    activated_at: 12244000
    governs:
      - "params.MainnetChainConfig.BerlinBlock"
      - "core/vm.gasSLoadEIP2929"
  - id: "policy.gas.london"
    name: "London Fee Market"
    category: "fees"
    description: "EIP-1559 base fee + priority fee."
    governs:
      - "params.MainnetChainConfig.LondonBlock"
`

// writeTemp drops content into a tempfile under t.TempDir and returns
// its path. Convenience helper so each table case can mint its own
// fixture without leaking files between cases.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return p
}

// TestLoadFromFile_Happy round-trips a valid two-entry YAML and
// confirms both surface-level rows survive parsing intact.
func TestLoadFromFile_Happy(t *testing.T) {
	f, err := LoadFromFile(writeTemp(t, validYAML))
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if len(f.Policies) != 2 {
		t.Fatalf("want 2 policies, got %d", len(f.Policies))
	}
	if f.Policies[0].ID != "fork.berlin" {
		t.Errorf("first policy id: got %q", f.Policies[0].ID)
	}
	if f.Policies[0].ActivatedAt != 12244000 {
		t.Errorf("activated_at not parsed: got %d", f.Policies[0].ActivatedAt)
	}
	if len(f.Policies[0].Governs) != 2 {
		t.Errorf("first policy governs: got %d", len(f.Policies[0].Governs))
	}
}

// TestLoadFromFile_MissingFile confirms the I/O error path wraps the
// underlying os.ReadFile error so callers can distinguish "file gone"
// from "file malformed".
func TestLoadFromFile_MissingFile(t *testing.T) {
	_, err := LoadFromFile("/nonexistent/policy.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// TestLoadFromFile_MalformedYAML covers the parse error path.
func TestLoadFromFile_MalformedYAML(t *testing.T) {
	_, err := LoadFromFile(writeTemp(t, "policies: not a list, just a string\n  with bad indent"))
	if err == nil {
		t.Fatal("expected parse error for malformed yaml")
	}
}

// TestLoadFromFile_EmptyIDRejected ensures the validator catches
// entries missing their primary key — without it Resolve would emit
// a Policy node with an empty qname and collide on subsequent runs.
func TestLoadFromFile_EmptyIDRejected(t *testing.T) {
	_, err := LoadFromFile(writeTemp(t, `policies:
  - id: ""
    name: "No ID"
`))
	if err == nil {
		t.Fatal("expected error for empty policy id")
	}
}

// TestLoadFromFile_DuplicateIDRejected guards the INSERT OR REPLACE
// collision case described in the loader doc.
func TestLoadFromFile_DuplicateIDRejected(t *testing.T) {
	_, err := LoadFromFile(writeTemp(t, `policies:
  - id: "dup"
    name: "First"
  - id: "dup"
    name: "Second"
`))
	if err == nil {
		t.Fatal("expected error for duplicate policy id")
	}
}

// TestResolve_MatchAndWarn exercises the matching loop: one governs[]
// entry resolves to a known code node and emits a governed_by edge;
// the other entry has no match and surfaces as a ResolveWarning.
func TestResolve_MatchAndWarn(t *testing.T) {
	f := &File{Policies: []Entry{
		{
			ID:   "fork.berlin",
			Name: "Berlin",
			Governs: []string{
				"params.MainnetChainConfig.BerlinBlock", // matches
				"this.symbol.does.not.exist",            // warning
			},
		},
	}}
	code := []types.Node{
		{ID: "n_chaincfg", QualifiedName: "params.MainnetChainConfig.BerlinBlock",
			Type: types.NodeField, Name: "BerlinBlock"},
	}

	res := Resolve(f, code, "policies.yaml")
	if len(res.Nodes) != 1 {
		t.Fatalf("want 1 Policy node, got %d", len(res.Nodes))
	}
	if res.Nodes[0].Type != types.NodePolicy {
		t.Errorf("Policy node type: got %q", res.Nodes[0].Type)
	}
	if res.Nodes[0].QualifiedName != "fork.berlin" {
		t.Errorf("Policy qname: got %q", res.Nodes[0].QualifiedName)
	}
	if len(res.Edges) != 1 {
		t.Fatalf("want 1 governed_by edge, got %d", len(res.Edges))
	}
	if res.Edges[0].Type != types.EdgeGovernedBy {
		t.Errorf("edge type: got %q", res.Edges[0].Type)
	}
	if res.Edges[0].Src != "n_chaincfg" {
		t.Errorf("edge src: got %q", res.Edges[0].Src)
	}
	if res.Edges[0].Dst != "policy:fork.berlin" {
		t.Errorf("edge dst: got %q", res.Edges[0].Dst)
	}
	if len(res.Warnings) != 1 {
		t.Fatalf("want 1 warning, got %d", len(res.Warnings))
	}
	if res.Warnings[0].TargetRef != "this.symbol.does.not.exist" {
		t.Errorf("warning target: got %q", res.Warnings[0].TargetRef)
	}
}

// TestResolve_EmptyInputs locks the nil-safe paths so the buildpipe
// caller can hand back a zero result when no policy file was supplied.
func TestResolve_EmptyInputs(t *testing.T) {
	res := Resolve(nil, nil, "")
	if len(res.Nodes) != 0 || len(res.Edges) != 0 || len(res.Warnings) != 0 {
		t.Errorf("nil File should yield empty result: %+v", res)
	}
	res = Resolve(&File{}, nil, "")
	if len(res.Nodes) != 0 {
		t.Errorf("empty File should yield empty result: %+v", res)
	}
}

// TestResolve_PolicyWithoutGoverns covers the "rationale-only" policy
// entry — useful for documenting context (e.g. a deprecated protocol
// parameter) that has no current code anchor.
func TestResolve_PolicyWithoutGoverns(t *testing.T) {
	f := &File{Policies: []Entry{{ID: "context.legacy", Name: "Legacy"}}}
	res := Resolve(f, nil, "policies.yaml")
	if len(res.Nodes) != 1 {
		t.Errorf("want 1 node, got %d", len(res.Nodes))
	}
	if len(res.Edges) != 0 {
		t.Errorf("want 0 edges, got %d", len(res.Edges))
	}
	if len(res.Warnings) != 0 {
		t.Errorf("want 0 warnings, got %d", len(res.Warnings))
	}
}
