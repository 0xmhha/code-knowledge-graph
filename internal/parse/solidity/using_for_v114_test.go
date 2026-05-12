package solidity_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	sol "github.com/0xmhha/code-knowledge-graph/internal/parse/solidity"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V1.14 — cross-file struct validation for the V1.10-V1.13
// family. Validates that the global structFieldTypes index (built by
// sweeping all PendingRefs across all ParseResults in Pass 2) plus
// the binding map and library function index all resolve correctly
// when struct types, contracts, and library live in distinct files.
//
// Confidence expectation (§2.2): cross-file resolution → ConfInferred.
//
// V1.14 carry-over (V1.x+):
//   - Multi-return tuple destructuring (statement-level local-var tracking)
//   - V1.10-V1.13 generic-walker subsumption (V2+ refactor)
//   - Grammar-blocked items (free-function form, file-level using)

// parseResolveMultiSol — shared helper for cross-file tests. Parses
// each file in `files` (relative to `dir`) and runs Resolve once on
// the combined results. Mirrors the multi-file pattern used by
// TestSolInheritance_CrossFile and TestSolDispatch_CrossFile.
func parseResolveMultiSol(t *testing.T, dir string, files []string) ([]types.Node, []types.Edge) {
	t.Helper()
	p := sol.New(dir)
	results := make([]*parse.ParseResult, 0, len(files))
	for _, f := range files {
		src, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		r, err := p.ParseFile(filepath.Join(dir, f), src)
		if err != nil {
			t.Fatalf("ParseFile %s: %v", f, err)
		}
		results = append(results, r)
	}
	resolved, err := p.Resolve(results)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return resolved.Nodes, resolved.Edges
}

// findUsingForCall returns the EdgeCalls edge matching (caller, target)
// or zero-value with ok=false. Scans by QualifiedName so cross-file ID
// hashing doesn't matter.
func findUsingForCall(nodes []types.Node, edges []types.Edge, caller, target string) (types.Edge, bool) {
	qnameByID := map[string]string{}
	for _, n := range nodes {
		qnameByID[n.ID] = n.QualifiedName
	}
	for _, e := range edges {
		if e.Type != types.EdgeCalls {
			continue
		}
		if qnameByID[e.Src] == caller && qnameByID[e.Dst] == target {
			return e, true
		}
	}
	return types.Edge{}, false
}

// TestUsingForV114_CrossFileStructFieldV10 — V1.10 depth-1 struct-field
// receiver where the struct AND library live in a different file from
// the caller contract. Walker must thread through global indices and
// emit ConfInferred (cross-file).
func TestUsingForV114_CrossFileStructFieldV10(t *testing.T) {
	dir := filepath.Join("testdata", "using_for_v114")
	files := []string{"cross_file_lib.sol", "cross_file_vault10.sol"}
	nodes, edges := parseResolveMultiSol(t, dir, files)
	edge, ok := findUsingForCall(nodes, edges, "Vault10.run", "SafeMath.add")
	if !ok {
		t.Fatalf("missing EdgeCalls Vault10.run → SafeMath.add across files")
	}
	if edge.Confidence != types.ConfInferred {
		t.Errorf("cross-file V1.10 EdgeCalls confidence: got %v, want ConfInferred",
			edge.Confidence)
	}
}

// TestUsingForV114_CrossFileThisNestedV13 — V1.13 this-prefixed depth-2
// chain where the struct types AND library live in a different file
// from the caller contract. Exercises this-prefixed walker (stateVar
// resolved via stateVarTypes) combined with cross-file struct chain
// resolution.
func TestUsingForV114_CrossFileThisNestedV13(t *testing.T) {
	dir := filepath.Join("testdata", "using_for_v114")
	files := []string{"cross_file_lib.sol", "cross_file_vault13.sol"}
	nodes, edges := parseResolveMultiSol(t, dir, files)
	edge, ok := findUsingForCall(nodes, edges, "Vault13.run", "SafeMath.add")
	if !ok {
		t.Fatalf("missing EdgeCalls Vault13.run → SafeMath.add across files")
	}
	if edge.Confidence != types.ConfInferred {
		t.Errorf("cross-file V1.13 EdgeCalls confidence: got %v, want ConfInferred",
			edge.Confidence)
	}
}
