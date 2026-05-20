package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W10 V12 — payable(this).transfer / payable(this).send is a
// self-reentrant value-transfer surface. The V12 walker
// extension accepts value-transfer method names alongside the
// existing low-level set, so self-cast value transfers light up
// HasSelfReentrantCall. Arbitrary-address value transfers stay
// on the W8 V1 HasValueTransfer path and do NOT trigger
// HasExternalCall (the marker is reserved for low-level calls).
func TestExternalCallCast_SelfValueTransferMarker(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/yul_receiver", "sol_self_value_transfer.sol")

	type triple struct {
		reentrant, externalCall, valueXfer bool
	}
	want := map[string]triple{
		"SelfTransfer.selfTransfer": {reentrant: true, externalCall: false, valueXfer: true},
		"SelfTransfer.selfSend":     {reentrant: true, externalCall: false, valueXfer: true},
		"SelfTransfer.externalSend": {reentrant: false, externalCall: false, valueXfer: true},
	}
	got := map[string]triple{}
	for _, n := range nodes {
		if n.Type != types.NodeFunction {
			continue
		}
		if _, ok := want[n.QualifiedName]; ok {
			got[n.QualifiedName] = triple{
				reentrant:    n.HasSelfReentrantCall,
				externalCall: n.HasExternalCall,
				valueXfer:    n.HasValueTransfer,
			}
		}
	}
	for qn, w := range want {
		g := got[qn]
		if g != w {
			t.Errorf("%s markers: got %+v want %+v", qn, g, w)
		}
	}
}

// W-C W10 V9 — self-delegatecall additionally lights up
// HasSelfDelegatecallDead. delegatecall against the contract
// itself re-runs the same bytecode against the same storage and
// is essentially a no-op / bug. Other self-call methods (call /
// staticcall) only set HasSelfReentrantCall.
func TestExternalCallCast_SelfDelegatecallDeadMarker(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/yul_receiver", "sol_self_delegate.sol")

	type pair struct {
		reentrant, dead bool
	}
	want := map[string]pair{
		"SelfDelegate.deadDelegate":    {reentrant: true, dead: true},
		"SelfDelegate.reentrantStatic": {reentrant: true, dead: false},
	}
	got := map[string]pair{}
	for _, n := range nodes {
		if n.Type != types.NodeFunction {
			continue
		}
		if _, ok := want[n.QualifiedName]; ok {
			got[n.QualifiedName] = pair{
				reentrant: n.HasSelfReentrantCall,
				dead:      n.HasSelfDelegatecallDead,
			}
		}
	}
	for qn, w := range want {
		g := got[qn]
		if g != w {
			t.Errorf("%s markers: got %+v want %+v", qn, g, w)
		}
	}
}

// W-C W10 V8 — self-reference cast routes to HasSelfReentrantCall
// instead of HasExternalCall. `payable(this).call(...)` and
// `address(this).call(...)` re-enter the same contract — the
// security signal is reentrancy, not arbitrary-address dispatch.
// Arbitrary-address cast keeps the V5 HasExternalCall behaviour.
func TestExternalCallCast_SelfReferenceMarker(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/yul_receiver", "sol_self_cast.sol")

	type pair struct {
		external, selfReentrant bool
	}
	want := map[string]pair{
		"SelfCaster.reentrant":     {external: false, selfReentrant: true},
		"SelfCaster.addressSelf":   {external: false, selfReentrant: true},
		"SelfCaster.externalRelay": {external: true, selfReentrant: false},
		"SelfCaster.noop":          {external: false, selfReentrant: false},
	}
	got := map[string]pair{}
	for _, n := range nodes {
		if n.Type != types.NodeFunction {
			continue
		}
		if _, ok := want[n.QualifiedName]; ok {
			got[n.QualifiedName] = pair{
				external:      n.HasExternalCall,
				selfReentrant: n.HasSelfReentrantCall,
			}
		}
	}
	for qn, w := range want {
		g := got[qn]
		if g != w {
			t.Errorf("%s markers: got %+v want %+v", qn, g, w)
		}
	}
}

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
