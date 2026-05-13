package solidity_test

import (
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V2.10 — mixed bare + aliased entries in single import.
//
// V1.28 introduced importAliases handling for named-import alias form
// `import {Lib as L} from "./lib.sol"` and the multi-aliased variant
// `import {LibA as A, LibB as B}`. V2.10 closes the heterogeneous-
// entry blind spot: one import statement where ONE entry has `as
// Alias` and another DOES NOT:
//
//   import {SafeMath, Address as A} from "./mixed_libs.sol";
//
// The walker must:
//   (a) Skip recording an alias for the bare entry (SafeMath) — the
//       bare name should keep its identity as-is for downstream
//       byName lookup.
//   (b) Record `A → Address` for the aliased entry.
//   (c) Not off-by-one between positional fields when the bare entry
//       has no `alias` child but the aliased entry does.

// TestUsingForV2100_MixedBareAndAliasedImport — both `using SafeMath`
// (bare) and `using A` (aliased) inside the same contract must emit
// EdgeUsesFor pointing to the original libraries.
func TestUsingForV2100_MixedBareAndAliasedImport(t *testing.T) {
	dir := filepath.Join("testdata", "using_for_v2100")
	files := []string{"mixed_libs.sol", "mixed_caller.sol"}
	nodes, edges := parseResolveMultiSol(t, dir, files)

	qnameByID := map[string]string{}
	for _, n := range nodes {
		qnameByID[n.ID] = n.QualifiedName
	}

	wants := map[string]bool{
		"Vault->SafeMath": false, // bare entry
		"Vault->Address":  false, // aliased entry, resolved via A → Address
	}
	for _, e := range edges {
		if e.Type != types.EdgeUsesFor {
			continue
		}
		key := qnameByID[e.Src] + "->" + qnameByID[e.Dst]
		if _, ok := wants[key]; ok {
			wants[key] = true
		}
	}
	for k, hit := range wants {
		if !hit {
			t.Errorf("missing EdgeUsesFor %s (V2.10 mixed bare/aliased import)", k)
		}
	}

	// Negative guard: the bare entry's name (`SafeMath`) must NOT be
	// recorded as an alias key. If it were, the walker would have
	// translated `using SafeMath for uint256` into some other library
	// or dropped it, leaving the Vault→SafeMath edge missing — which
	// the positive assertion above already catches. This explicit
	// negative also rejects any phantom Vault → A edge (where `A` is
	// the alias name, not a library).
	for _, e := range edges {
		if e.Type != types.EdgeUsesFor {
			continue
		}
		dst := qnameByID[e.Dst]
		if dst == "A" {
			t.Errorf("phantom EdgeUsesFor Vault → A (alias name leaked as library, V2.10 regression): %+v", e)
		}
	}
}

// TestUsingForV2100_MixedImportDispatchBarePath — V1.0 state-var
// dispatch must work for the BARE-imported library. Confirms the
// alias machinery doesn't break the non-aliased code path.
func TestUsingForV2100_MixedImportDispatchBarePath(t *testing.T) {
	dir := filepath.Join("testdata", "using_for_v2100")
	files := []string{"mixed_libs.sol", "mixed_caller.sol"}
	nodes, edges := parseResolveMultiSol(t, dir, files)

	edge, ok := findUsingForCall(nodes, edges, "Vault.compute", "SafeMath.add")
	if !ok {
		t.Fatalf("missing EdgeCalls Vault.compute → SafeMath.add (V2.10 bare-import dispatch path)")
	}
	if edge.Confidence != types.ConfInferred {
		t.Errorf("cross-file V2.10 bare-path EdgeCalls confidence: got %v, want ConfInferred",
			edge.Confidence)
	}
}
