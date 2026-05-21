package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W10 V20 — try-wrapped high-level self-call audit. Locks the
// composition of two independent walker properties:
//
//   - V16 (try_statement transparency): nearestFunctionQnameAndStart
//     walks through control-flow nodes and reaches the enclosing
//     function_definition, so any marker keyed on the function ID
//     fires regardless of try-wrapping.
//   - V19 (HasHighLevelSelfCall): runHighLevelSelfCallMarker queries
//     the whole tree for member_expression nodes and uses isSelfRef
//     to recognise this / cast-wrapped this.
//
// Their composition (try around a high-level self-call) should
// require no additional code — but cross-axis behaviour is the
// kind of thing that silently breaks when a refactor introduces
// a scope window on either walker. V20 makes the invariant explicit
// so a future regression on either axis fails here.
//
// Two fixtures exercise both V19 shapes inside a try block:
//
//   - TryHighSelf.spawn        : try this.target() {} catch {}
//     bare `this.foo()` dispatch.
//   - TryHighInterface.trigger : try IFoo(address(this)).foo() {} catch {}
//     interface-type cast dispatch (isSelfRef recursion through
//     the call_expression branch + address(this) inner cast).
//
// Both functions should produce HasHighLevelSelfCall=true. The
// low-level markers stay false (V19 doesn't set them; the bodies
// contain no .call/.transfer surface).
func TestTryHighLevel_SelfCallMarker(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/yul_receiver",
		"sol_try_high_level_self_call.sol")

	want := map[string]bool{
		"TryHighSelf.spawn":        true,
		"TryHighInterface.trigger": true,
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
		// Cross-axis assertion: V19's marker is the only one that
		// should fire here. V8/V18 (HasSelfReentrantCall,
		// HasExternalCall) belong to the low-level surface and must
		// stay false on these typed-only fixtures.
		if n.HasSelfReentrantCall {
			t.Errorf("%s HasSelfReentrantCall: got true (V20 expects false; high-level only)",
				n.QualifiedName)
		}
		if n.HasExternalCall {
			t.Errorf("%s HasExternalCall: got true (V20 expects false; high-level is not arbitrary-address dispatch)",
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
