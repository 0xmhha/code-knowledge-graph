package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W8 V27 — cast-receiver / helper-return chain negative lock.
//
// V26 (chained_cross_contract.sol) covered the `s.getCb()(x)` shape
// where the outer callee is itself a call_expression. V6
// (cross_contract_fn_pointer.go) documents two more shapes as
// deferred:
//
//   - Cast-receiver: `Hub(addr).onAction(x)` — receiver is a
//     contract-cast call_expression.
//   - Helper-return-receiver: `getHub().onAction(x)` — receiver is
//     an internal helper's return value.
//
// Both fall through matchStateVarMethodCall's bare-identifier
// receiver requirement. V27 pins all three rows in one fixture:
//
//   - bareInvoke (reference)   : HasFunctionPointerCall=true   (V6 cover)
//   - castInvoke   (cell A)    : HasFunctionPointerCall=false  (V6 drop)
//   - helperInvoke (cell B)    : HasFunctionPointerCall=false  (V6 drop)
//
// The reference row is essential: it pins V6's positive baseline
// inside the same fixture so the negative cells are *specifically*
// about receiver-shape variance from the bare-identifier case, not
// about V6 propagation being broken generally. If V6 ever regresses
// on the bare path, bareInvoke fails and the diagnosis is
// "general regression," not "negative locks unexpectedly flipped."
//
// Cross-flip protocol: if any of the three rows flips, both this
// test value and WALKER_SYMMETRY drift row 5 must move together —
// the catalogue lists V26 + V27 as the *same* limitation family.
func TestW8V27_CastAndHelperReceiver_NegativeLock(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/function_typed_var",
		"cast_and_helper_receiver.sol")

	type cell struct {
		ptrCall bool
		ptrProp bool
	}
	want := map[string]cell{
		// Reference row: V6 positive baseline.
		"Caller.bareInvoke": {ptrCall: true, ptrProp: false},
		// Cell A: cast-receiver shape.
		"Caller.castInvoke": {ptrCall: false, ptrProp: false},
		// Cell B: helper-return-receiver shape.
		"Caller.helperInvoke": {ptrCall: false, ptrProp: false},
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
			t.Errorf("%s: got %+v, want %+v\n  (if a walker change flipped this row on purpose, flip the want value AND update WALKER_SYMMETRY drift row 5 — V26 and V27 are the same limitation family and must move together)",
				qn, g, w)
		}
	}
}
