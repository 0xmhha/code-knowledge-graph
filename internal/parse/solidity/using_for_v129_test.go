package solidity_test

import (
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V1.29 — whole-file alias qualified using directive.
//
// Solidity's `import "./util.sol" as L; using L.SafeMath for uint256;`
// pattern: L is a namespace alias for the whole imported file, and
// L.SafeMath qualifies the library reference. Tree-sitter parses the
// type_alias child of using_directive as a sequence of identifiers —
// [L, SafeMath] for the qualified form. The V0 query
// `(type_alias (identifier) @lib)` matches only the first identifier,
// so pre-V1.29 the captured library name is "L" which doesn't exist
// as a library declaration → binding miss → dispatch drop.
//
// V1.29 walks all identifier children of type_alias and uses the
// LAST one (the unqualified library name). The global byName index
// (resolve.go) already keys NodeContract by Name field across all
// files, so name-only lookup hits the actual library node.
//
// V1.29 carry-over (V1.30+):
//   - Cross-file disambiguation when two files declare libraries with
//     the same Name — currently V0 picks first via pickSameFileCandidate;
//     whole-file alias's L should narrow to a specific source file
//     for correctness in such cases. V0 simplification accepts the
//     first-hit behavior.
//   - Block-scoped shadowing precision (V2+ refactor).
//   - Grammar-blocked items.

// TestUsingForV129_WholeFileAliasQualified — canonical V1.29 case.
// Caller imports lib under whole-file alias L and uses-for via
// `L.SafeMath`. Expected: alias prefix discarded, SafeMath resolved
// through the global byName index.
func TestUsingForV129_WholeFileAliasQualified(t *testing.T) {
	dir := filepath.Join("testdata", "using_for_v129")
	files := []string{"whole_file_alias_lib.sol", "whole_file_alias_caller.sol"}
	nodes, edges := parseResolveMultiSol(t, dir, files)
	edge, ok := findUsingForCall(nodes, edges, "Vault.compute", "SafeMath.add")
	if !ok {
		t.Fatalf("missing EdgeCalls Vault.compute → SafeMath.add (V1.29 whole-file alias)")
	}
	if edge.Confidence != types.ConfInferred {
		t.Errorf("cross-file V1.29 whole-file alias EdgeCalls confidence: got %v, want ConfInferred",
			edge.Confidence)
	}
}

// TestUsingForV129_WholeFileAliasCollision — collision-risk guard.
// An unrelated file declares contract `L` (same name as the whole-
// file alias used by Vault). Pre-V1.29 the runUsingFor walker emits
// a PendingRef for the namespace prefix `L`, and Pass 2's strict-
// purge filters it out only because no library/contract named L
// exists. With this collision fixture, byName[NodeContract]["L"]
// resolves to the unrelated contract → false-positive EdgeUsesFor
// (Vault → L) absent V1.29's namespace-alias tracking. Expectation:
// V1.29 must skip emitting PendingRefs for known namespace aliases.
func TestUsingForV129_WholeFileAliasCollision(t *testing.T) {
	dir := filepath.Join("testdata", "using_for_v129")
	files := []string{
		"whole_file_alias_lib.sol",
		"whole_file_alias_caller.sol",
		"collision_other.sol",
	}
	nodes, edges := parseResolveMultiSol(t, dir, files)
	qnameByID := map[string]string{}
	for _, n := range nodes {
		qnameByID[n.ID] = n.QualifiedName
	}
	for _, e := range edges {
		if e.Type != types.EdgeUsesFor {
			continue
		}
		if qnameByID[e.Src] == "Vault" && qnameByID[e.Dst] == "L" {
			t.Errorf("false-positive EdgeUsesFor: Vault → L (V1.29 namespace-alias collision)")
		}
	}
	// SafeMath edge must still exist.
	if _, ok := findUsingForCall(nodes, edges, "Vault.compute", "SafeMath.add"); !ok {
		t.Errorf("missing EdgeCalls Vault.compute → SafeMath.add (V1.29 collision guard)")
	}
}
