package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V1.9 — `this.<state-var>.<method>` receiver tests.
//
// `this` is Sol's implicit current-contract reference. V1.9 strips the
// `this` prefix and treats the inner property as a state-var on the
// caller's container — semantically equivalent to V1.0's bare-name
// `<state-var>.<method>` shape, just stylistic.
//
// V1.9 reuses V1.0's dispatch kind + resolver (encoding identical) so
// the resolution path is single-source. New predicate only.

// TestUsingForV19_ThisStateVar — canonical V1.9 case. `this.balance.add(1)`
// resolves through Vault's state var binding to ThisLib.add.
func TestUsingForV19_ThisStateVar(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v19", "this_state_var.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "Vault.run", target: "ThisLib.add"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (using-for V1.9 this) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV19_ThisNoStateVar — `this.<unknown-field>.method()`
// drops at stateVarTypes lookup. Catches false positives where V1.9
// might inadvertently emit for any `this.<id>.<id>` shape.
func TestUsingForV19_ThisNoStateVar(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v19", "this_no_state_var.sol")
	got := collectUsingForCalls(nodes, edges)
	if len(got) != 0 {
		t.Errorf("unexpected V1.9 EdgeCalls when this.field is not a state var: %v", got)
	}
}

// TestUsingForV19_ThisVsBare — both `value.touch()` (V1.0) and
// `this.value.touch()` (V1.9) resolve to the same library edge. They
// produce two distinct call sites — the graph records both.
func TestUsingForV19_ThisVsBare(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v19", "this_vs_bare.sol")
	count := 0
	byID := map[string]types.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	for _, e := range edges {
		if e.Type != types.EdgeCalls {
			continue
		}
		// Filter to library-target edges (SampleLib).
		if byID[e.Dst].QualifiedName == "SampleLib.touch" &&
			byID[e.Src].QualifiedName == "Sample.run" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 using-for EdgeCalls (V1.0 bare + V1.9 this), got %d", count)
	}
}
