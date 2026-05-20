package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W10 V15 — constructor self-cast audit. Constructors are
// emitted via runConstructorDecl (W6 V1.23) as NodeFunction with
// SubKind="constructor" and a synthetic identifier. The V5/V8/V12
// cast walker reaches the enclosing callable through
// nearestFunctionQnameAndStart, which already recognises
// constructor_definition — so HasSelfReentrantCall should fire on
// the constructor row when the body contains
// `payable(this).call(...)` or `.transfer(...)`.
//
// V15 locks the coverage. A regression that drops
// constructor_definition from the enclosing-callable walk, or
// that diverges runConstructorDecl's startByte from the
// nearestFunctionQnameAndStart computation, would silently lose
// the reentrancy signal on every constructor — a high-value
// blind spot since constructors run with privileged state and
// self-calls during init are textbook footguns.
func TestConstructor_SelfReentrantCast(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/yul_receiver",
		"sol_constructor_self_cast.sol")

	want := map[string]bool{
		"InitSelf.constructor":     true,
		"InitTransfer.constructor": true,
	}
	got := map[string]bool{}
	for _, n := range nodes {
		if n.Type != types.NodeFunction || n.SubKind != "constructor" {
			continue
		}
		if _, ok := want[n.QualifiedName]; ok {
			got[n.QualifiedName] = n.HasSelfReentrantCall
			if n.HasExternalCall {
				t.Errorf("%s HasExternalCall: got true (V15 expects false; self-cast routes to reentrant)",
					n.QualifiedName)
			}
		}
	}
	for qn, w := range want {
		g, present := got[qn]
		if !present {
			t.Errorf("missing constructor %q", qn)
			continue
		}
		if g != w {
			t.Errorf("%s HasSelfReentrantCall: got %v, want %v", qn, g, w)
		}
	}
}
