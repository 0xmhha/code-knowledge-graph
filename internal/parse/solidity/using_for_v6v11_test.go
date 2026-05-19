package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V11 — using directive whose target is an interface.
// solc rejects this construction; vendored tree-sitter still
// parses it. resolveUsingForRef looks up byName[NodeContract]
// (and falls back to NodeFunction for free functions), neither
// of which includes interfaces. The PendingRef should drop
// silently and no EdgeUsesFor should appear on the Caller
// contract.
//
// V11 locks the absence of a false-positive resolution. A future
// regression that broadens the resolver to include
// byName[NodeInterface] would silently emit the (semantically
// wrong) edge and fail this test.
func TestUsingForV6V11_UsingAgainstInterface(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v6v11", "using_interface.sol")

	byID := map[string]types.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	var usesFor []types.Edge
	for _, e := range edges {
		if e.Type == types.EdgeUsesFor {
			usesFor = append(usesFor, e)
		}
	}
	for _, e := range usesFor {
		t.Errorf("unexpected EdgeUsesFor (interface target): %s -> %s",
			byID[e.Src].Name, byID[e.Dst].Name)
	}

	// Surround-safety: every named declaration must still
	// index. A regression that crashed the directive's parse
	// would drop downstream nodes.
	wantNodes := []string{"IFoo", "IFoo.ping", "Caller", "Caller.probe"}
	seen := map[string]bool{}
	for _, n := range nodes {
		seen[n.QualifiedName] = true
	}
	for _, qn := range wantNodes {
		if !seen[qn] {
			t.Errorf("surround-safety: %q not indexed", qn)
		}
	}
}
