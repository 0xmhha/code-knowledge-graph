package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W10 V17 — modifier-scope self-cast audit. Modifiers are emitted
// via runDecl(NodeModifier) and the cast walker (W10 V5/V8/V12) keys
// the HasSelfReentrantCall marker on the (qname, startByte) pair that
// nearestFunctionQnameAndStart returns. Since W6 V1.22 taught that
// helper to recognise modifier_definition as an enclosing-callable
// shape, the marker should land on the NodeModifier row when the
// body contains payable(this).call(...) or .transfer(...).
//
// V17 locks the coverage. Modifiers wrapping other functions are a
// classic re-entrancy vector: a self-call inside `_; payable(this).call(...)`
// re-enters the wrapped function before its state-mutation hooks
// settle. The marker must surface that surface independently of the
// wrapped function's own markers.
//
// Two contracts exercise the two W10 admission paths:
//
//   - GuardSelf.reentrantGuard   : property=="call",
//     inner=payable(this) → selfAffected.
//   - GuardTransfer.refundGuard  : property=="transfer", admitted by
//     the V12 value-transfer branch alongside V8 low-level-call.
//
// Both modifier rows should end up with HasSelfReentrantCall=true and
// HasExternalCall=false — the V8 precedence rule still applies on
// NodeModifier (the walker doesn't gate on node type).
func TestModifier_SelfReentrantCast(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/yul_receiver",
		"sol_modifier_self_cast.sol")

	want := map[string]bool{
		"GuardSelf.reentrantGuard":  true,
		"GuardTransfer.refundGuard": true,
	}
	got := map[string]bool{}
	for _, n := range nodes {
		if n.Type != types.NodeModifier {
			continue
		}
		if _, ok := want[n.QualifiedName]; !ok {
			continue
		}
		got[n.QualifiedName] = n.HasSelfReentrantCall
		if n.HasExternalCall {
			t.Errorf("%s HasExternalCall: got true (V17 expects false; self-cast routes to reentrant)",
				n.QualifiedName)
		}
	}
	for qn, w := range want {
		g, present := got[qn]
		if !present {
			t.Errorf("missing modifier %q", qn)
			continue
		}
		if g != w {
			t.Errorf("%s HasSelfReentrantCall: got %v, want %v", qn, g, w)
		}
	}
}
