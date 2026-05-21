package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W10 V21 — chained-receiver self-call NEGATIVE lockdown.
//
// V19's isSelfRef recognises three self-ref shapes:
//
//	identifier "this"                       (bare)
//	payable / address / type_cast(<self>)   (cast wrapper)
//	call_expression with upper-case leading // (contract / interface
//	  identifier + single call_argument     //   cast convention)
//
// The third branch's upper-case rule rejects `helper(this).foo()`
// style calls. That's an intentional false negative: Sol's grammar
// can't distinguish a contract / interface cast from a regular
// function call by syntax alone, and the upper-case convention
// trims the false-positive surface (`requireOwner(this).foo()`
// would otherwise mark every modifier-style helper). The cost is
// missing a real self-call when the helper actually returns
// `this`, as in this fixture.
//
// The negative lockdown:
//
//   - Fixture: ChainedSelf.invoke calls
//     `getTarget(this).foo()`. The helper returns its
//     argument unchanged, so this is *semantically* a typed
//     self-call.
//   - Test: HasHighLevelSelfCall is expected to be **false**.
//
// Two regression paths flip this:
//
//  1. The heuristic is loosened (accept lower-case leading
//     identifiers). The test will fail; the maintainer must
//     weigh the new false positives against the recovered true
//     positives and explicitly update either the heuristic or
//     this expectation.
//
//  2. A real cross-procedural recognition lands (e.g. return-
//     type analysis of `getTarget` proves it returns `this`).
//     The test will fail and the maintainer flips the
//     expectation to true.
//
// Without this lockdown the trade is implicit — a refactor that
// changes the heuristic flips the marker silently and the cks-
// side downstream sees recall move without an obvious cause.
func TestChainedReceiver_NoSelfMarker(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/yul_receiver",
		"sol_chained_receiver_self_call.sol")

	target := "ChainedSelf.invoke"
	found := false
	for _, n := range nodes {
		if n.Type != types.NodeFunction || n.QualifiedName != target {
			continue
		}
		found = true
		if n.HasHighLevelSelfCall {
			t.Errorf("%s HasHighLevelSelfCall: got true, want false (intentional V19 false-negative — see fixture header for the heuristic trade)",
				n.QualifiedName)
		}
		// Cross-axis: low-level markers must also stay false on a
		// chained-receiver high-level call. V8 / V18 don't admit
		// helper(...).foo() either (it's not a low-level method),
		// but pinning both keeps the negative-axis assertion
		// symmetric with V19 / V20 / V22.
		if n.HasSelfReentrantCall {
			t.Errorf("%s HasSelfReentrantCall: got true, want false (low-level marker should not fire on high-level chained call)",
				n.QualifiedName)
		}
		if n.HasExternalCall {
			t.Errorf("%s HasExternalCall: got true, want false (chained dispatch with internal helper)",
				n.QualifiedName)
		}
	}
	if !found {
		t.Errorf("missing function %q", target)
	}
}
