package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W10 V16 — try-statement self-cast audit. The cast walker
// (W10 V5/V8/V12) finds every member_expression via a tree-sitter
// query and resolves enclosing callable through
// nearestFunctionQnameAndStart, which walks the parent chain looking
// for function_definition / modifier_definition / constructor_definition /
// fallback_receive_definition. try_statement is none of those — it's
// a control-flow node — so the walker should treat it as transparent
// and attach the self-reentrant marker to the *outer* function.
//
// V16 locks the coverage. The pattern is high-value because the
// "I wrapped it in try, so re-entrancy doesn't matter" misconception
// is common; the marker exists precisely to call out that the inner
// callee can still re-enter the caller before try completes. A
// regression that introduced a try-scoped query window, or that
// stopped nearestFunctionQnameAndStart at try_statement, would
// silently drop this signal on every try-wrapped self-call surface.
//
// Two contracts exercise the two admission paths in W10:
//
//   - TryCall.attack   : member-expression query → property=="call",
//     inner=payable(this) cast → selfAffected.
//   - TryTransfer.drain: same query → property=="transfer", admitted
//     by the V12 value-transfer branch alongside
//     the V8 low-level-call branch.
//
// Both functions should end up with HasSelfReentrantCall=true and
// HasExternalCall=false — the V8 precedence rule ("self-cast routes
// to reentrant, not external").
func TestTryStatement_SelfReentrantCast(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/yul_receiver",
		"sol_try_self_cast.sol")

	want := map[string]bool{
		"TryCall.attack":    true,
		"TryTransfer.drain": true,
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
			t.Errorf("%s HasExternalCall: got true (V16 expects false; self-cast routes to reentrant)",
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
