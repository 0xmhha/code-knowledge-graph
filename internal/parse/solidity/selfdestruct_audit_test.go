package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W10 V13 — selfdestruct audit. Sol's `selfdestruct(addr)` is
// a top-level builtin call, not a member-expression on a
// receiver, so the existing W10 V8/V9/V12 cast walkers (which
// match `<cast>.<method>(...)`) don't see it. The W10 V0
// HasAssembly / W8 V1 HasLowLevelCall markers also don't fire
// because the call form is a bare identifier, not an inline
// assembly block or member-expression low-level call.
//
// V13 locks the current behaviour: selfdestruct(this) does NOT
// surface on any of HasExternalCall, HasSelfReentrantCall, or
// HasSelfDelegatecallDead. Treating selfdestruct as its own
// dispatch surface is a separate W10 V14+ scope decision.
func TestSelfdestructAudit_NoMarkerFalsePositive(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/yul_receiver", "selfdestruct_this.sol")

	for _, qn := range []string{"SelfDestruct.destroyToSelf", "SelfDestruct.destroyToAddress"} {
		var fn types.Node
		for _, n := range nodes {
			if n.QualifiedName == qn && n.Type == types.NodeFunction {
				fn = n
				break
			}
		}
		if fn.ID == "" {
			t.Errorf("missing %q in graph", qn)
			continue
		}
		if fn.HasExternalCall {
			t.Errorf("%s HasExternalCall: got true (V13 expected false)", qn)
		}
		if fn.HasSelfReentrantCall {
			t.Errorf("%s HasSelfReentrantCall: got true (V13 expected false)", qn)
		}
		if fn.HasSelfDelegatecallDead {
			t.Errorf("%s HasSelfDelegatecallDead: got true (V13 expected false)", qn)
		}
	}
}
