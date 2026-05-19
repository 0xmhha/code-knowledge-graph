package solidity_test

import (
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V8 — method name propagation on EdgeUsesFor.DispatchKind.
// The using-for walkers carry the method name from `Lib.method`
// forms (operator-form or namespace-aliased nested path) through
// to Pass 2 via a TargetQName suffix encoded with the RFC record
// separator. resolveUsingForRef decodes it and surfaces the value
// on Edge.DispatchKind as `using_for|<method>` so downstream
// consumers can see which library member the binding targets.
//
// Backward compatibility: bindings that lack a method name (e.g.
// the legacy `using Lib for T;` form) keep the bare `using_for`
// DispatchKind value.
func TestUsingForV6V8_MethodOnDispatchKind(t *testing.T) {
	nodes, edges := parseResolveOneSol(t,
		"testdata/using_for_v2200", "operator_form_recovery.sol")

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
	if len(usesFor) == 0 {
		t.Fatalf("no EdgeUsesFor in fixture")
	}

	// Every emitted edge in operator_form_recovery.sol should carry
	// `using_for|add` because each `using {Math.add as +}` directive
	// names `add` as the bound method.
	for _, e := range usesFor {
		src := byID[e.Src].Name
		dst := byID[e.Dst].Name
		if !strings.HasPrefix(e.DispatchKind, "using_for") {
			t.Errorf("%s -> %s: DispatchKind missing using_for prefix: %q", src, dst, e.DispatchKind)
			continue
		}
		// Method portion is everything after the first "|".
		if idx := strings.Index(e.DispatchKind, "|"); idx < 0 {
			t.Errorf("%s -> %s: missing method suffix in DispatchKind %q", src, dst, e.DispatchKind)
		} else if method := e.DispatchKind[idx+1:]; method != "add" {
			t.Errorf("%s -> %s: method on DispatchKind: got %q, want %q", src, dst, method, "add")
		}
	}
}
