package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W10 V5 — cast / wrapper shapes (`address(x).call(...)` and
// `payable(x).call(...)`) light up HasExternalCall on the enclosing
// callable. The cast itself is sufficient evidence of arbitrary-
// address dispatch, so V5 marks at Pass 1 without going through
// receiver-type resolution like V4 does for bare-identifier
// receivers.
func TestExternalCallCast_CastReceiverMarker(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/yul_receiver", "sol_cast_external.sol")

	want := map[string]bool{
		"Caster.viaAddressCast": true,
		"Caster.viaPayableCast": true,
		"Caster.safeStore":      false,
	}
	got := map[string]bool{}
	for _, n := range nodes {
		if n.Type != types.NodeFunction {
			continue
		}
		if _, ok := want[n.QualifiedName]; ok {
			got[n.QualifiedName] = n.HasExternalCall
		}
	}
	for qn, w := range want {
		if got[qn] != w {
			t.Errorf("HasExternalCall on %q: got %v want %v", qn, got[qn], w)
		}
	}
}
