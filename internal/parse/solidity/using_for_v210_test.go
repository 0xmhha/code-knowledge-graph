package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V2.1 — interface receiver via using-for regression guard.
//
// Common DeFi pattern: a contract holds a state variable typed as an
// interface (e.g. IERC20, IToken), with a `using <Lib> for <Interface>`
// directive in scope. Method calls on the interface-typed receiver
// dispatch through the bound library, which takes the interface as
// its first parameter.
//
// V0/V1.0 binding map keys typeName as a plain string, so interface
// types should work uniformly with primitive / struct types. byName
// indexing for the library lookup uses NodeContract (libraries are
// indexed as NodeContract with SubKind="library" per W4); the
// interface receiver type doesn't change anything about that lookup.
//
// V2.1 locks in this expected behavior so future refactors don't
// accidentally exclude interface-typed receivers.
//
// V2.1 carry-over (V2.2+):
//   - Multi-binding for same type (`using A for uint256; using B for
//     uint256;`) — V0 bindings overwrite on second directive.
//   - Array / mapping as bound type.
//   - Byte-range precision (line refinement).
//   - Module/import handling additional patterns.

// TestUsingForV210_InterfaceReceiver — state-var typed as interface
// dispatches via library bound to that interface. Expected EdgeCalls:
// Vault.run → HelperLib.balanceOf.
func TestUsingForV210_InterfaceReceiver(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v210", "interface_receiver.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "Vault.run", target: "HelperLib.balanceOf"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V2.1 interface receiver) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV210_MultiBindingResolves — V2.2 multi-binding fix.
// `using A for uint256; using B for uint256;` 둘 다 적용. uint256.tag()
// 가 LibA.tag 로 resolve (LibA 가 tag method 보유, LibB 는 보유 안 함
// — V2.2 resolveBindingLib 가 hit 까지 iterate).
//
// V2.1 originally locked this as known limitation (single-value
// binding, second-wins overwrite). V2.2 promoted bindings to
// `map[typeName][]libraryName` so all directives' libraries are
// preserved and resolution tries each until method hit. V2.1 의
// `_KnownLimitation` test name 도 함께 변경.
func TestUsingForV210_MultiBindingResolves(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v210", "multi_binding_same_type.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "Vault.run", target: "LibA.tag"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V2.2 multi-binding resolves) mismatch:\n got=%v\nwant=%v", got, want)
	}
	// Defensive: both EdgeUsesFor edges (Vault → LibA, Vault → LibB)
	// should land regardless of method-dispatch resolution.
	qnameByID := map[string]string{}
	for _, n := range nodes {
		qnameByID[n.ID] = n.QualifiedName
	}
	seen := map[string]bool{}
	for _, e := range edges {
		if e.Type == types.EdgeUsesFor && qnameByID[e.Src] == "Vault" {
			seen[qnameByID[e.Dst]] = true
		}
	}
	for _, lib := range []string{"LibA", "LibB"} {
		if !seen[lib] {
			t.Errorf("missing EdgeUsesFor Vault → %s (V2.2 multi-binding)", lib)
		}
	}
}
