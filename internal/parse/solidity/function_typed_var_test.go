package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W8 V2 — function-typed state variable marker.
//
// V0 detects shape: state_variable_declaration.type_name has a
// `parameter` or `return_parameter` child (the function-type
// signature components) instead of the usual primitive / user-
// defined / mapping shape. Sets NodeField.IsFunctionTyped = true.
//
// V0 limitations: call-site resolution `handler(args)` against
// function-typed state vars is not modelled (no function-type
// tracking in lookupReceiverType yet). Adding such tracking is
// the natural V3+ extension once a consumer needs it.

func TestFunctionTypedVar_Marker(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/function_typed_var", "registry.sol")

	want := map[string]bool{
		"Registry.handler":  true,
		"Registry.callback": true,
		"Registry.counter":  false,
		"Registry.owner":    false,
	}

	got := map[string]bool{}
	seen := map[string]bool{}
	for _, n := range nodes {
		if n.Type != types.NodeField {
			continue
		}
		if _, ok := want[n.QualifiedName]; !ok {
			continue
		}
		seen[n.QualifiedName] = true
		got[n.QualifiedName] = n.IsFunctionTyped
	}

	for qn, w := range want {
		if !seen[qn] {
			t.Errorf("W8 V2 missing NodeField %q", qn)
			continue
		}
		if got[qn] != w {
			t.Errorf("W8 V2 NodeField %q IsFunctionTyped: got %v, want %v", qn, got[qn], w)
		}
	}
}
