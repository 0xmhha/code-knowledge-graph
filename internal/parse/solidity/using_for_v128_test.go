package solidity_test

import (
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V1.28 — aliased import resolution.
//
// Solidity's named-import alias form `import {SafeMath as SM} from
// "./util.sol"` brings the library SafeMath into the importing file's
// namespace under the alias SM. `using SM for uint256;` then expects
// the binding map to associate uint256 with SM, but SM is not a
// library declaration — it's an alias for SafeMath. Pre-V1.28 the
// runUsingFor walker looked up the bare identifier SM in
// byName[NodeContract], missed (no library named SM), and dropped the
// binding → all SM dispatch sites also dropped.
//
// V1.28 closes the gap by:
//   1. Walking import_directive nodes during the parse pass and
//      recording per-file (alias → original) pairs from the positional
//      `alias` / `import_name` fields.
//   2. In runUsingFor, applying the alias map to the captured library
//      identifier before pushing onto the binding pipeline. The binding
//      target QName carries the *original* library name so Pass 2
//      resolution (byName[NodeContract][libraryName]) sees the correct
//      key.
//
// V1.28 carry-over (V1.29+):
//   - Whole-file alias (`import "./util.sol" as L; L.SafeMath.add` —
//     qualified access). Requires extending dispatch to qualified
//     identifier lookups.
//   - Block-scoped shadowing precision (V2+ refactor).
//   - Grammar-blocked items.

// TestUsingForV128_AliasedNamedImport — canonical V1.28 case. Caller
// imports SafeMath under alias SM and uses-for via SM. Expects the
// alias to resolve to SafeMath transparently.
func TestUsingForV128_AliasedNamedImport(t *testing.T) {
	dir := filepath.Join("testdata", "using_for_v128")
	files := []string{"aliased_lib.sol", "aliased_caller.sol"}
	nodes, edges := parseResolveMultiSol(t, dir, files)
	edge, ok := findUsingForCall(nodes, edges, "Vault.compute", "SafeMath.add")
	if !ok {
		t.Fatalf("missing EdgeCalls Vault.compute → SafeMath.add (V1.28 alias) across files")
	}
	if edge.Confidence != types.ConfInferred {
		t.Errorf("cross-file V1.28 alias EdgeCalls confidence: got %v, want ConfInferred",
			edge.Confidence)
	}
}

// TestUsingForV128_MultiAliasedImport — `import {SafeMath as SM,
// Address as A} from "..."` — multiple aliases in one directive. Both
// EdgeUsesFor edges must surface (Vault → SafeMath, Vault → Address)
// after alias resolution. Confirms the per-file map handles multiple
// entries.
func TestUsingForV128_MultiAliasedImport(t *testing.T) {
	dir := filepath.Join("testdata", "using_for_v128")
	files := []string{"multi_lib.sol", "multi_caller.sol"}
	nodes, edges := parseResolveMultiSol(t, dir, files)
	idByName := map[string]string{}
	for _, n := range nodes {
		if n.Type == types.NodeContract {
			idByName[n.QualifiedName] = n.ID
		}
	}
	wants := map[string]bool{
		"Vault->SafeMath": false,
		"Vault->Address":  false,
	}
	qnameByID := map[string]string{}
	for _, n := range nodes {
		qnameByID[n.ID] = n.QualifiedName
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
			t.Errorf("missing EdgeUsesFor %s (V1.28 multi alias)", k)
		}
	}
}
