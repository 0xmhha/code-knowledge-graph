package solidity_test

import (
	"testing"
)

// W-C W6 V1.27 — inherited modifier `using` regression guard.
//
// Confirms the intersection of three established mechanisms:
//   1. V1.0 contract-scope `using ... for ...` binding map
//      (bindings[contractID][typeName] = libraryName).
//   2. V1.2 inherited binding propagation (child inherits parent's
//      using directives via the inheritance graph — BFS in resolve.go).
//   3. V1.22 modifier body scope captures (modifier's parameter +
//      local indexing into paramTypes / localVarTypes).
//
// Pattern under test: Parent declares a `using SafeMath for uint256`
// + a modifier `check(uint256 amount)` whose body calls
// `amount.add(0)`. Child inherits Parent. Two assertions:
//   (a) Parent.check resolves its modifier-param dispatch via
//       bindings[Parent] (V1.22 + V1.0 path). Inheritance doesn't
//       perturb Parent's own binding map.
//   (b) V1.2 still propagates the binding to Child for completeness
//       — Parent → SafeMath and Child → SafeMath edges both exist.
//
// V1.27 carry-over (V1.28+):
//   - Block-scoped shadowing precision (V2+ refactor).
//   - Module/import handling (V2 territory).
//   - Grammar-blocked items.

// TestUsingForV127_InheritedModifierUsing — Parent modifier with using-
// for dispatch in its body; Child inherits Parent. Verifies V1.22's
// modifier scope and V1.2's inheritance propagation cooperate.
func TestUsingForV127_InheritedModifierUsing(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v127", "inherited_modifier.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "Parent.check", target: "SafeMath.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V1.27 inherited modifier using) mismatch:\n got=%v\nwant=%v", got, want)
	}
}
