package solidity_test

import (
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V1.18 — cross-file tuple validation. V1.14 idiom applied to
// V1.16: a multi-file fixture where the library + struct types live
// in one file and the V1.16 tuple-destructuring caller lives in
// another. Confirms the localVarTypes / structFieldTypes / bindings
// indices (built by Pass 2 sweeping all ParseResults) resolve across
// file boundaries at ConfInferred.
//
// V1.18 carry-over (V1.19+):
//   - Block-scoped shadowing precision (V2+ refactor).
//   - Grammar-blocked items (free-function form, file-level using).

// TestUsingForV118_CrossFileTupleDestructure — V1.16 tuple destructuring
// with both slots typed. UserData struct and SafeMath library live in
// cross_file_lib.sol; the caller Vault.run lives in cross_file_vault.sol.
// Walker chains: tuple slot u → UserData (cross-file) → balance
// (cross-file struct field) → uint256 → SafeMath (cross-file library)
// → add. Expected: 1 EdgeCalls Vault.run → SafeMath.add at ConfInferred.
func TestUsingForV118_CrossFileTupleDestructure(t *testing.T) {
	dir := filepath.Join("testdata", "using_for_v118")
	files := []string{"cross_file_lib.sol", "cross_file_vault.sol"}
	nodes, edges := parseResolveMultiSol(t, dir, files)
	edge, ok := findUsingForCall(nodes, edges, "Vault.run", "SafeMath.add")
	if !ok {
		t.Fatalf("missing EdgeCalls Vault.run → SafeMath.add across files")
	}
	if edge.Confidence != types.ConfInferred {
		t.Errorf("cross-file V1.18 tuple EdgeCalls confidence: got %v, want ConfInferred",
			edge.Confidence)
	}
}
