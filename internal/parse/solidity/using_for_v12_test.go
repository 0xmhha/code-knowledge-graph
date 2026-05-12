package solidity_test

import (
	"sort"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V1.2 — inherited using directive tests.
//
// Solidity contracts can `is` other contracts; the child inherits the
// parent's `using` declarations (in practice — solc treats them as
// in-scope). V1.2 propagates bindings down the inheritance graph by
// BFS over the parents adjacency built in Pass 2a, merging each
// ancestor's bindings into the descendant without clobbering local
// declarations.
//
// V1.2 carry-over note (file-level using): tree-sitter-solidity
// v1.2.13 produces ERROR nodes for 0.8.13+ source-file-scope using
// directives, so file-level scope is grammar-blocked. See
// queries.go's V1.2 limitation note. V1.2 lands the inherited path
// (well-supported by the grammar) instead.

// collectUsesForByContractInherited groups EdgeUsesFor edges into
// (contract, library) pairs — same shape as V1.2 file-level helper
// in spirit, but local to V1.2 inheritance tests so they don't have
// to depend on each other's file structure.
func collectUsesForByContractInherited(nodes []types.Node, edges []types.Edge) []callWant {
	byID := map[string]types.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	var out []callWant
	for _, e := range edges {
		if e.Type != types.EdgeUsesFor {
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

// TestUsingForV12_InheritedBasic — Single-level inheritance. Parent
// declares `using ParentLib for uint256;`; Child inherits and uses
// `value.inc()` where value is Child's state variable.
//
// Confirms:
//   - EdgeUsesFor remains on Parent only (not synthesised for Child)
//     — V1.2 propagates the binding map but doesn't replicate the
//     declaration edge.
//   - EdgeCalls (Child.bump → ParentLib.inc) resolves through the
//     inherited binding.
func TestUsingForV12_InheritedBasic(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v12", "inherited_basic.sol")

	usesFor := collectUsesForByContractInherited(nodes, edges)
	wantUses := []callWant{
		{caller: "Parent", target: "ParentLib"},
	}
	if !equalCallWants(usesFor, wantUses) {
		t.Errorf("EdgeUsesFor mismatch:\n got=%v\nwant=%v\n"+
			"(V1.2 propagates binding via map; should NOT replicate Parent→ParentLib for Child)",
			usesFor, wantUses)
	}

	calls := collectUsingForCalls(nodes, edges)
	wantCalls := []callWant{
		{caller: "Child.bump", target: "ParentLib.inc"},
	}
	if !equalCallWants(calls, wantCalls) {
		t.Errorf("EdgeCalls (using-for) mismatch:\n got=%v\nwant=%v\n"+
			"(Child must dispatch through inherited ParentLib binding)",
			calls, wantCalls)
	}
}

// TestUsingForV12_InheritedMultiLevel — Grandparent's binding reaches a
// grandchild through a parent that didn't redeclare. Exercises the BFS
// transitive walk (Child → Parent → Grand).
func TestUsingForV12_InheritedMultiLevel(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v12", "inherited_multi_level.sol")

	usesFor := collectUsesForByContractInherited(nodes, edges)
	wantUses := []callWant{
		{caller: "Grand", target: "GrandLib"},
	}
	if !equalCallWants(usesFor, wantUses) {
		t.Errorf("EdgeUsesFor mismatch:\n got=%v\nwant=%v", usesFor, wantUses)
	}

	calls := collectUsingForCalls(nodes, edges)
	wantCalls := []callWant{
		{caller: "Child.tap2", target: "GrandLib.tap"},
		{caller: "Parent.tap", target: "GrandLib.tap"},
	}
	if !equalCallWants(calls, wantCalls) {
		t.Errorf("EdgeCalls (using-for) mismatch:\n got=%v\nwant=%v\n"+
			"(both Parent and Child must reach GrandLib via inheritance)",
			calls, wantCalls)
	}
}

// TestUsingForV12_InheritedChildOverrides — Child's own `using ChildLib
// for uint256;` shadows P's inherited `using InheritedLib for uint256;`
// for matching typeNames. Local declaration wins per Solidity scoping
// (and per the V1.2 BFS rule "don't clobber a child-scope binding").
func TestUsingForV12_InheritedChildOverrides(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v12", "inherited_child_overrides.sol")

	usesFor := collectUsesForByContractInherited(nodes, edges)
	wantUses := []callWant{
		{caller: "C", target: "ChildLib"},
		{caller: "P", target: "InheritedLib"},
	}
	if !equalCallWants(usesFor, wantUses) {
		t.Errorf("EdgeUsesFor mismatch:\n got=%v\nwant=%v", usesFor, wantUses)
	}

	calls := collectUsingForCalls(nodes, edges)
	wantCalls := []callWant{
		{caller: "C.run", target: "ChildLib.tag"},
	}
	if !equalCallWants(calls, wantCalls) {
		t.Errorf("EdgeCalls (using-for) mismatch:\n got=%v\nwant=%v\n"+
			"(child-scope binding must shadow inherited binding for matching uint256)",
			calls, wantCalls)
	}
}
