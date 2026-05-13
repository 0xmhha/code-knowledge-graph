package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V2.13 — diamond inheritance + multi-parent using-for
// binding union.
//
// V1.2 introduced BFS-based inheritance propagation for binding
// maps so a child contract picks up its parents' `using` directives
// transparently. V2.2 then extended bindings to multi-value to
// support `contract Foo { using A for T; using B for T; }` within
// a single contract.
//
// V2.13 probes the intersection: when a child inherits from two
// parents A and B that BOTH bind the same type T to different
// libraries, do both bindings survive in the child's binding map?
//
// Pre-V2.13 code: the BFS "don't clobber" branch (resolve.go
// ~L385) writes `bindings[childID][typeName] = cloned` only when
// the slot doesn't already exist. After the first ancestor's
// binding sets the slot, subsequent ancestors' bindings for the
// same type are dropped — child-shadows-inherited semantics
// applied incorrectly to peer-ancestors at the SAME inheritance
// depth.
//
// Expected dispatch surface:
//   - Child.callAdd → SafeMath.add (inherited from A)
//   - Child.callMul → OtherMath.mul (inherited from B)
//
// Both should fire because the child's receiver `x` is uint256
// and both libraries contribute methods on uint256.

// TestUsingForV2130_DiamondInheritedBinding — multi-parent
// inherited binding union check.
func TestUsingForV2130_DiamondInheritedBinding(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v2130", "diamond_using.sol")

	qnameByID := map[string]string{}
	for _, n := range nodes {
		qnameByID[n.ID] = n.QualifiedName
	}

	// (a) Direct EdgeUsesFor on each parent.
	wantUF := map[string]bool{
		"A->SafeMath":  false,
		"B->OtherMath": false,
	}
	for _, e := range edges {
		if e.Type != types.EdgeUsesFor {
			continue
		}
		key := qnameByID[e.Src] + "->" + qnameByID[e.Dst]
		if _, ok := wantUF[key]; ok {
			wantUF[key] = true
		}
	}
	for k, hit := range wantUF {
		if !hit {
			t.Errorf("missing EdgeUsesFor %s (V2.13 parent direct binding)", k)
		}
	}

	// (b) Inherited dispatch: child's receiver method calls must
	// resolve through BOTH parents' bindings, not just one.
	gotAdd := false
	gotMul := false
	for _, e := range edges {
		if e.Type != types.EdgeCalls {
			continue
		}
		src := qnameByID[e.Src]
		dst := qnameByID[e.Dst]
		if src == "Child.callAdd" && dst == "SafeMath.add" {
			gotAdd = true
		}
		if src == "Child.callMul" && dst == "OtherMath.mul" {
			gotMul = true
		}
	}
	if !gotAdd {
		t.Errorf("missing EdgeCalls Child.callAdd → SafeMath.add (V2.13 inherited from A)")
	}
	if !gotMul {
		t.Errorf("missing EdgeCalls Child.callMul → OtherMath.mul (V2.13 inherited from B — multi-parent union)")
	}
}
