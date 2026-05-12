package solidity_test

import (
	"testing"
)

// W-C W6 V1.25 — free function regression guard.
//
// Sol 0.7.4+ allows file-level `function` declarations outside any
// contract / library / interface — so-called free functions. They
// share the function_definition AST kind with member functions but
// nearestContractName returns "" for them, so the qname has no
// container prefix and Pass 1.5 containerIDByFuncID never gains an
// entry. Consequently lookupReceiverType cannot resolve a contract-
// scope `using ... for ...` binding from inside a free function —
// which matches Sol's language rule: `using` directives are
// contract-scope only.
//
// V1.25 locks in the expected behavior:
//   - free function parses without errors
//   - free function emits no phantom EdgeCalls into the using-for
//     dispatch path
//   - contract using-for dispatch in the same file is unaffected
//
// V1.25 carry-over (V1.26+):
//   - abstract contract body scope captures.
//   - inherited modifier `using` baseline.
//   - Block-scoped shadowing precision (V2+ refactor).
//   - Module/import handling (V2 territory).

// TestUsingForV125_FreeFunctionAndContract — fixture has one free
// function + one contract with using-for. Only the contract's call
// site should emit EdgeCalls; the free function's body has no binding
// scope and emits no using-for EdgeCalls.
func TestUsingForV125_FreeFunctionAndContract(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v125", "free_function.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "C.useIt", target: "SafeMath.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (V1.25 free function alongside contract) mismatch:\n got=%v\nwant=%v", got, want)
	}
}
