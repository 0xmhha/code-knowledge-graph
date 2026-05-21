package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W10 V18 — call-options chained self-cast audit. Modern Solidity
// (0.7+) writes value/gas overrides as `.call{value: x, gas: y}(...)`
// rather than the deprecated `.call.value(x).gas(y)()` chain. The
// grammar introduces a call_options_expression between the member
// expression and the outer call_expression — so the V8 walker has
// to step through that node to reach call_expression. Before V18
// the walker bailed at the call_expression check and dropped the
// security signal on every modern self-call.
//
// The blind spot was *real*, not theoretical: cks corpora indexed
// against this branch silently lost HasSelfReentrantCall on the most
// common modern syntax. V18 fixes runExternalCallCastMarker to also
// step through call_options_expression, and this fixture locks the
// behaviour.
//
// Two functions exercise the two option shapes:
//
//   - OptCall.fire    : .call{value: 0}("")  — value-bearing options
//   - OptCall.fireGas : .call{gas: 50000}("") — gas-only options
//
// Both functions must end up HasSelfReentrantCall=true and
// HasExternalCall=false (V8 precedence rule, unchanged).
func TestCallOptions_SelfReentrantCast(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/yul_receiver",
		"sol_call_options_self_cast.sol")

	want := map[string]bool{
		"OptCall.fire":    true,
		"OptCall.fireGas": true,
	}
	got := map[string]bool{}
	for _, n := range nodes {
		if n.Type != types.NodeFunction {
			continue
		}
		if _, ok := want[n.QualifiedName]; !ok {
			continue
		}
		got[n.QualifiedName] = n.HasSelfReentrantCall
		if n.HasExternalCall {
			t.Errorf("%s HasExternalCall: got true (V18 expects false; self-cast routes to reentrant)",
				n.QualifiedName)
		}
	}
	for qn, w := range want {
		g, present := got[qn]
		if !present {
			t.Errorf("missing function %q", qn)
			continue
		}
		if g != w {
			t.Errorf("%s HasSelfReentrantCall: got %v, want %v", qn, g, w)
		}
	}
}
