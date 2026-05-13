package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V2.6 — using-alias free-function form regression guard.
//
// Empirical finding: V0 queryUsingFor incidentally captures the
// `Math` identifier from `using {Math.add, Math.sub} for uint256;`
// (contract-scoped free-function form). Tree-sitter v1.2.13's
// parse is clean enough that the V0 query's
// `(using_directive (type_alias (identifier) @lib) ...)` shape
// matches against the grammar's actual structure for this case.
//
// This invalidates part of V0's grammar-blocked carry-over: the
// claim "`{Math.add, Math.sub}` brace shape parses to ERROR" is
// false for v1.2.13. V2.5's 0-edges result for `using {mul as *}
// for uint256 global;` was due to file-level scope being excluded
// by queryUsingFor (only matches contract / library / interface
// bodies), not because using_alias handling is broken.
//
// V2.6 locks the rediscovered behavior:
//   (a) Contract-scoped `using {Lib.f1, Lib.f2} for T;` emits 1
//       EdgeUsesFor (Caller → Lib) — V0 query captures Lib.
//   (b) The using-alias binding successfully feeds V1.0 dispatch:
//       receivers of type T resolve through Lib for method-call
//       dispatch.
//
// V2.6 carry-over (V2.7+):
//   - File-level using directives (still excluded from queryUsingFor).
//   - Operator-form `using {f as +}` at contract scope (V2.5 covered
//     file-level operator-form; contract-scope variant TBD).
//   - Byte 정밀도 / module-import 추가 / Grammar-blocked items.

// TestUsingForV260_FreeFunctionFormBinding — `using {Math.add,
// Math.sub} for uint256;` at contract scope. V0 query incidentally
// matches `Math` as @lib token. Verifies Caller → Math EdgeUsesFor.
func TestUsingForV260_FreeFunctionFormBinding(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v260", "probe_using_alias.sol")
	qnameByID := map[string]string{}
	for _, n := range nodes {
		qnameByID[n.ID] = n.QualifiedName
	}
	seen := false
	for _, e := range edges {
		if e.Type == types.EdgeUsesFor &&
			qnameByID[e.Src] == "Calc" && qnameByID[e.Dst] == "Math" {
			seen = true
		}
	}
	if !seen {
		t.Errorf("missing EdgeUsesFor Calc → Math (V2.6 free-function form binding)")
	}
}

// TestUsingForV260_FreeFunctionFormDispatch — same form + actual
// receiver method-call dispatch. Verifies V1.0 dispatch chain works
// through the using-alias-emitted binding.
func TestUsingForV260_FreeFunctionFormDispatch(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v260", "free_function_dispatch.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "Vault.run", target: "Math.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V2.6 using-alias + V1.0 dispatch) mismatch:\n got=%v\nwant=%v", got, want)
	}
}
