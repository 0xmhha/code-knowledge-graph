package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W10 V10 — Yul-level self-delegatecall surfaces as
// HasSelfDelegatecallDead, parallel to the V9 Sol-level marker.
// `delegatecall(gas(), address(), …)` is a dead-weight
// operation since `address()` resolves to the executing
// contract's own address. Regular Yul calls (`call(gas, target,
// …)`) only set HasLowLevelCall.
func TestYulSelfDelegatecallDeadMarker(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/yul_receiver", "yul_self_delegate.sol")

	type pair struct {
		lowLevel, dead bool
	}
	want := map[string]pair{
		"YulSelfDelegate.deadYulDelegate": {lowLevel: true, dead: true},
		"YulSelfDelegate.normalYulCall":   {lowLevel: true, dead: false},
	}
	got := map[string]pair{}
	for _, n := range nodes {
		if n.Type != types.NodeFunction {
			continue
		}
		if _, ok := want[n.QualifiedName]; ok {
			got[n.QualifiedName] = pair{
				lowLevel: n.HasLowLevelCall,
				dead:     n.HasSelfDelegatecallDead,
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
