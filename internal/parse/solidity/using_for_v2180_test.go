package solidity_test

import (
	"sort"
	"testing"
)

// W-C W6 V2.18 — file-level using directive ERROR-tolerant walker
// (V2.16 row 1 closure).
//
// Sol 0.8.13+ `using LibName for T [global];` at source_file scope
// is grammar-blocked in vendored tree-sitter-solidity v1.2.11. The
// V2.18 probe (2026-05-17) showed the directive parses into a
// single ERROR child of source_file with recoverable identifiers:
//
//   source_file
//     ERROR "using SafeMath for uint256 [global];"
//       type_name (user_defined_type (identifier "using"))   ← keyword
//       identifier "SafeMath"                                ← lib name
//       type_name (primitive_type "uint256")                 ← bound type
//       [identifier "global"]                                ← optional
//
// V2.18 deliverable: runFileLevelUsingFor walker pattern-matches
// this ERROR shape and emits the same PendingRef pair that
// runUsingFor uses for contract-body using directives. Binding fans
// out per-contract-in-file so Sol's file-level semantics ("applies
// to all contracts in this source") are preserved end-to-end through
// dispatch resolution.
//
// V2.16 row 1 status flip: A (grammar-block) → A-recovered.
// V2.5 (file-level operator-form) stays at 0 — V2.17 analysis showed
// no recoverable shape for `as +` variant at any scope.

// TestUsingForV2180_FileLevelUsingGlobal — `using SafeMath for
// uint256 global;` at source_file scope produces 1 EdgeUsesFor
// (Vault → SafeMath) and dispatch wires through (`x.add(1)` →
// SafeMath.add). Locks V2.16 row 1 closure for the single-contract
// case.
func TestUsingForV2180_FileLevelUsingGlobal(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v2180", "file_level_using_global.sol")

	// (a) EdgeUsesFor: Vault → SafeMath.
	gotUF := collectUsingFor(nodes, edges)
	wantUF := []usingForWant{{contract: "Vault", library: "SafeMath"}}
	if !equalUsingFor(gotUF, wantUF) {
		t.Errorf("V2.18 EdgeUsesFor mismatch: got=%v want=%v", gotUF, wantUF)
	}

	// (b) EdgeCalls: Vault.compute → SafeMath.add (dispatch wiring).
	gotCalls := collectUsingForCalls(nodes, edges)
	wantCalls := []callWant{{caller: "Vault.compute", target: "SafeMath.add"}}
	if !equalCallWants(gotCalls, wantCalls) {
		t.Errorf("V2.18 EdgeCalls dispatch mismatch: got=%v want=%v", gotCalls, wantCalls)
	}

	// (c) Surround-safety: SafeMath, SafeMath.add, Vault, Vault.compute
	// all still index.
	want := map[string]bool{
		"SafeMath":      false,
		"SafeMath.add":  false,
		"Vault":         false,
		"Vault.compute": false,
	}
	for _, n := range nodes {
		if _, ok := want[n.QualifiedName]; ok {
			want[n.QualifiedName] = true
		}
	}
	for qn, seen := range want {
		if !seen {
			t.Errorf("V2.18 surround-safety: declaration %q not indexed", qn)
		}
	}
}

// TestUsingForV2180_FileLevelUsingMultiContract — file-level using
// fans out across every contract in the same file. Asserts 2
// EdgeUsesFor + 2 EdgeCalls (one per contract). Validates "applies
// to all contracts" semantics.
func TestUsingForV2180_FileLevelUsingMultiContract(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v2180", "file_level_using_multi_contract.sol")

	// (a) EdgeUsesFor: 2 edges, one per contract.
	gotUF := collectUsingFor(nodes, edges)
	wantUF := []usingForWant{
		{contract: "VaultA", library: "SafeMath"},
		{contract: "VaultB", library: "SafeMath"},
	}
	sort.Slice(gotUF, func(i, j int) bool {
		if gotUF[i].contract != gotUF[j].contract {
			return gotUF[i].contract < gotUF[j].contract
		}
		return gotUF[i].library < gotUF[j].library
	})
	if !equalUsingFor(gotUF, wantUF) {
		t.Errorf("V2.18 multi-contract EdgeUsesFor mismatch: got=%v want=%v", gotUF, wantUF)
	}

	// (b) EdgeCalls: 2 dispatch edges, one per compute().
	gotCalls := collectUsingForCalls(nodes, edges)
	wantCalls := []callWant{
		{caller: "VaultA.compute", target: "SafeMath.add"},
		{caller: "VaultB.compute", target: "SafeMath.add"},
	}
	if !equalCallWants(gotCalls, wantCalls) {
		t.Errorf("V2.18 multi-contract EdgeCalls mismatch: got=%v want=%v", gotCalls, wantCalls)
	}
}
