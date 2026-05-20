package solidity_test

import (
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V2.4 — cross-file multi-binding regression guard. V2.2 added
// multi-value binding semantics (`using A for T; using B for T;`) +
// resolveBindingLib helper that iterates the multi-bound libraries
// to find the one with the requested method. V2.4 validates this
// works across file boundaries — libs in one file, caller in another.
//
// V2.4 carry-over (V2.5+):
//   - Byte-range precision (V2.0 line-based fallback).
//   - Module/import additional patterns.
//   - Grammar-blocked items.

// TestUsingForV240_CrossFileMultiBinding — LibA + LibB defined in
// cross_file_libs.sol; Vault in cross_file_caller.sol uses both via
// multi-binding. Each method exists in exactly one library:
//   - LibA.tag → resolveBindingLib finds tag on LibA
//   - LibB.bump → resolveBindingLib finds bump on LibB
//
// Cross-file resolution → ConfInferred.
func TestUsingForV240_CrossFileMultiBinding(t *testing.T) {
	dir := filepath.Join("testdata", "using_for_v240")
	files := []string{"cross_file_libs.sol", "cross_file_caller.sol"}
	nodes, edges := parseResolveMultiSol(t, dir, files)

	wantCalls := []callWant{
		{caller: "Vault.run", target: "LibA.tag"},
		{caller: "Vault.run", target: "LibB.bump"},
	}
	got := collectUsingForCalls(nodes, edges)
	if !equalCallWants(got, wantCalls) {
		t.Errorf("EdgeCalls (V2.4 cross-file multi-binding) mismatch:\n got=%v\nwant=%v", got, wantCalls)
	}

	// Both EdgeCalls must be ConfInferred (cross-file).
	for _, e := range edges {
		if e.Type != types.EdgeCalls {
			continue
		}
		if e.Confidence != types.ConfInferred {
			t.Errorf("cross-file V2.4 EdgeCalls confidence: got %v, want ConfInferred (edge=%+v)",
				e.Confidence, e)
		}
	}

	// Both EdgeUsesFor must also land (Vault → LibA, Vault → LibB).
	qnameByID := map[string]string{}
	for _, n := range nodes {
		qnameByID[n.ID] = n.QualifiedName
	}
	seenLibs := map[string]bool{}
	for _, e := range edges {
		if e.Type == types.EdgeUsesFor && qnameByID[e.Src] == "Vault" {
			seenLibs[qnameByID[e.Dst]] = true
		}
	}
	for _, lib := range []string{"LibA", "LibB"} {
		if !seenLibs[lib] {
			t.Errorf("missing EdgeUsesFor Vault → %s (V2.4 cross-file multi-binding)", lib)
		}
	}
}
