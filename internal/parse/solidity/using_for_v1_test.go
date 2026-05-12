package solidity_test

import (
	"sort"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V1.0 — method-call dispatch resolution tests.
//
// These tests exercise the V1.0 chain: state-var type extraction →
// per-contract binding map → method-call PendingRef → EdgeCalls emission.
//
// EdgeUsesFor (V0) assertions remain in using_for_test.go; this file
// focuses on EdgeCalls produced through the using_for_call resolver
// path, plus the receiver-type / binding-fallback edge cases.

type callWant struct {
	caller string // <Contract>.<method>
	target string // <Library>.<method>
}

// collectUsingForCalls extracts EdgeCalls edges whose Src is a Function
// node and Dst is a Function node in a Contract with SubKind="library".
// Filters out the parser's other call edges so the assertion focuses on
// the using_for_call branch only.
func collectUsingForCalls(nodes []types.Node, edges []types.Edge) []callWant {
	byID := map[string]types.Node{}
	libraryFuncIDs := map[string]bool{}
	libByID := map[string]string{} // contractID → contractName for libraries
	for _, n := range nodes {
		byID[n.ID] = n
		if n.Type == types.NodeContract && n.SubKind == "library" {
			libByID[n.ID] = n.Name
		}
	}
	// A Function's qname is "Library.method"; mark it as a library
	// function when its enclosing container qname appears in libByID.
	libNames := map[string]bool{}
	for _, name := range libByID {
		libNames[name] = true
	}
	for _, n := range nodes {
		if n.Type != types.NodeFunction {
			continue
		}
		// extract prefix before "."
		for i := 0; i < len(n.QualifiedName); i++ {
			if n.QualifiedName[i] == '.' {
				prefix := n.QualifiedName[:i]
				if libNames[prefix] {
					libraryFuncIDs[n.ID] = true
				}
				break
			}
		}
	}
	var out []callWant
	for _, e := range edges {
		if e.Type != types.EdgeCalls {
			continue
		}
		if !libraryFuncIDs[e.Dst] {
			continue
		}
		out = append(out, callWant{
			caller: byID[e.Src].QualifiedName,
			target: byID[e.Dst].QualifiedName,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].caller != out[j].caller {
			return out[i].caller < out[j].caller
		}
		return out[i].target < out[j].target
	})
	return out
}

func equalCallWants(a, b []callWant) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestUsingForV1_StateVarDispatch — canonical V1.0 case: state-variable
// receiver with specific-type binding. Both .add and .sub on the
// `balance` uint256 state var must resolve to SafeMath functions.
func TestUsingForV1_StateVarDispatch(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v1", "state_var_dispatch.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "Vault.deposit", target: "SafeMath.add"},
		{caller: "Vault.withdraw", target: "SafeMath.sub"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (using-for) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV1_WildcardDispatch — `using Lib for *` wildcard fallback
// fires when no specific binding exists for the receiver's type.
// counter (uint256) has no specific binding → wildcard AnyLib is used.
func TestUsingForV1_WildcardDispatch(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v1", "wildcard_dispatch.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "Universal.run", target: "AnyLib.boop"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (using-for) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV1_SpecificOverWildcard — Q9-3 (a) decision: when both a
// specific-type binding and a `for *` binding are declared in the same
// contract, the specific binding wins for matching receiver types.
//
// Fixture has both `using SpecificLib for uint256;` and
// `using FallbackLib for *;`. value (uint256) hits the specific path.
func TestUsingForV1_SpecificOverWildcard(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v1", "specific_over_wildcard.sol")
	got := collectUsingForCalls(nodes, edges)
	want := []callWant{
		{caller: "Specifics.touch", target: "SpecificLib.specificOp"},
	}
	if !equalCallWants(got, want) {
		t.Errorf("EdgeCalls (using-for) mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// TestUsingForV1_NoBindingNegative — defensive guard. A contract with
// no using directive (even if a library is declared in the same file)
// must produce zero using_for_call EdgeCalls. Catches a regression where
// the resolver fires without a binding-map entry.
func TestUsingForV1_NoBindingNegative(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v1", "no_binding_negative.sol")
	got := collectUsingForCalls(nodes, edges)
	if len(got) != 0 {
		t.Errorf("unexpected using-for EdgeCalls in no_binding_negative: %v", got)
	}
}
