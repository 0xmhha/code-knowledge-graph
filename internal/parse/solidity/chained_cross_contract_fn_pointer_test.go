package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W8 V26 — cross-contract function-pointer chain negative lock.
//
// V6 (cross_contract_fn_pointer.go) documents its own limitation:
// matchStateVarMethodCall requires a bare-identifier receiver, so
// chained / cast receiver shapes drop. `s.getCb()(x)` is the same
// limitation class — the callee of the outer call_expression is
// itself a call_expression, not a member_expression rooted at a
// state-var-typed identifier, so V6 never matches.
//
// V9 (return propagation) has a sibling limitation: it classifies a
// returned expression as fn-typed only when the expression is a
// bare identifier that resolves to a fn-typed param or state-var on
// the enclosing contract. `return s.getCb();` returns the result of
// a *cross-contract* call whose declared return type is fn-typed;
// V9 does not consult cross-contract return types when deciding
// whether to mark HasFunctionPointerPropagation.
//
// Both limitations are intentional V6/V9-era simplifications. V26
// pins them explicitly so:
//
//  1. A future walker change that adds chained-receiver tracking
//     OR cross-contract return-type tracking *fails this test* and
//     forces the author to flip these assertions to true — which
//     is exactly the visibility the V21 negative-lock pattern
//     provides for HasHighLevelSelfCall.
//
//  2. A future drive-by refactor that *accidentally* lights up
//     HasFunctionPointerCall on `s.getCb()(x)` (e.g. by widening
//     matchStateVarMethodCall to accept any call_expression
//     callee) without also handling Pass-2 receiver resolution
//     fails immediately instead of silently shipping a half-fix.
//
// The fixture also locks the reference rows: Source.getCb itself
// stays HasFunctionPointerPropagation=true (V9 still fires for the
// in-contract `return stored;` shape), so the negative lock on
// Sink.chainFetchOnly is *specifically about the chained-call
// return*, not about V9 propagation generally.
func TestW8V26_ChainedCrossContractFnPointer_NegativeLock(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/function_typed_var",
		"chained_cross_contract.sol")

	type cell struct {
		ptrCall bool
		ptrProp bool
	}
	want := map[string]cell{
		// V9 reference: in-contract `return stored;` still propagates.
		"Source.getCb": {ptrCall: false, ptrProp: true},
		// V26 negative-lock cell 1: chained-invoke miss.
		"Sink.chainInvoke": {ptrCall: false, ptrProp: false},
		// V26 negative-lock cell 2: chained-return-fetch miss.
		"Sink.chainFetchOnly": {ptrCall: false, ptrProp: false},
	}
	got := map[string]cell{}
	for _, n := range nodes {
		if n.Type != types.NodeFunction {
			continue
		}
		if _, ok := want[n.QualifiedName]; !ok {
			continue
		}
		got[n.QualifiedName] = cell{
			ptrCall: n.HasFunctionPointerCall,
			ptrProp: n.HasFunctionPointerPropagation,
		}
	}
	for qn, w := range want {
		g, present := got[qn]
		if !present {
			t.Errorf("missing function %q", qn)
			continue
		}
		if g != w {
			t.Errorf("%s: got %+v, want %+v\n  (if a walker change made one of these true on purpose, flip the want value here AND update WALKER_SYMMETRY drift row 5)",
				qn, g, w)
		}
	}
}
