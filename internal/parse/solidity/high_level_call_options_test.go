package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W10 V22 — high-level call-options self-call audit.
//
// V18 added a struct_expression hop on the *low-level* cast walker
// so `payable(this).call{value: x, gas: y}(...)` kept its
// HasSelfReentrantCall marker. V19 introduced the parallel
// high-level walker but copied the pre-V18 single-hop shape — so
// the typed-dispatch equivalent `this.foo{value: x}()` silently
// lost HasHighLevelSelfCall.
//
// The blind spot was real (probe confirmed before the fix): both
// `this.target{value: 0}()` and `this.target{gas: 50000}()`
// produced HasHighLevelSelfCall=false on a graph that should have
// fired the marker. V22 adds the same struct_expression hop to
// runHighLevelSelfCallMarker so the two walkers stay shape-
// consistent.
//
// The lock matters because the value-bearing dispatch shape is
// the most-suspect re-entrancy surface: an attacker who controls
// the inner callee + receives ETH can drain the caller before
// state mutations land. Missing this marker on the typed path
// means cks silently downgrades the re-entrancy signal on every
// payable workflow that uses options-style dispatch.
func TestHighLevelCallOptions_SelfMarker(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/yul_receiver",
		"sol_high_level_call_options_self.sol")

	want := map[string]bool{
		"OptHigh.fire":    true,
		"OptHigh.fireGas": true,
	}
	got := map[string]bool{}
	for _, n := range nodes {
		if n.Type != types.NodeFunction {
			continue
		}
		if _, ok := want[n.QualifiedName]; !ok {
			continue
		}
		got[n.QualifiedName] = n.HasHighLevelSelfCall
		// V19 stays a separate axis from V8 / V18 — the low-level
		// markers must stay false on typed-only fixtures.
		if n.HasSelfReentrantCall {
			t.Errorf("%s HasSelfReentrantCall: got true (V22 expects false; high-level marker only)",
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
			t.Errorf("%s HasHighLevelSelfCall: got %v, want %v", qn, g, w)
		}
	}
}
