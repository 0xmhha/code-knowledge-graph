package solidity_test

import (
	"sort"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W7.3 V0 — modifier composition: multi-modifier order + modifier
// override.
//
// V0 captures two previously-lost dimensions of EdgeHasModifier /
// EdgeOverrides:
//
//   - Order: for `function f() M1 M2 M3 {}` the 3 EdgeHasModifier
//     edges carry Order = 0 / 1 / 2 (source order). Sol semantics
//     apply modifiers outer-to-inner in source order, so this is
//     semantically meaningful (reentrancy guards vs access checks).
//
//   - Modifier override: `modifier m() override {}` in a child
//     contract emits an EdgeOverrides (child mod → parent mod) the
//     same way W2 emits override edges for function_definition with
//     override_specifier.

func TestModifierComposition_OrderAndOverride(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/modifier_composition", "order_and_override.sol")

	byID := map[string]types.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}

	// (a) EdgeHasModifier with Order populated. 3 edges, all from
	// Child.withdraw, ordered 0=nonReentrant, 1=onlyOwner, 2=checkAmount.
	type hasMod struct {
		caller string
		mod    string
		order  int
	}
	var got []hasMod
	for _, e := range edges {
		if e.Type != types.EdgeHasModifier {
			continue
		}
		if byID[e.Src].QualifiedName != "Child.withdraw" {
			continue
		}
		got = append(got, hasMod{
			caller: byID[e.Src].QualifiedName,
			mod:    byID[e.Dst].Name,
			order:  e.Order,
		})
	}
	sort.Slice(got, func(i, j int) bool { return got[i].order < got[j].order })
	want := []hasMod{
		{caller: "Child.withdraw", mod: "nonReentrant", order: 0},
		{caller: "Child.withdraw", mod: "onlyOwner", order: 1},
		{caller: "Child.withdraw", mod: "checkAmount", order: 2},
	}
	if len(got) != len(want) {
		t.Errorf("W7.3 EdgeHasModifier count: got %d, want %d (got=%v)", len(got), len(want), got)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("W7.3 EdgeHasModifier[%d]: got %+v, want %+v", i, got[i], want[i])
			}
		}
	}

	// (b) EdgeOverrides for modifier-pair. 2 expected:
	//     Child.onlyOwner    → Base.onlyOwner
	//     Child.checkAmount  → Base.checkAmount
	type ovr struct{ child, parent string }
	var ovrs []ovr
	for _, e := range edges {
		if e.Type != types.EdgeOverrides {
			continue
		}
		// only count modifier-pair edges (skip any function overrides if
		// the fixture had them — this one doesn't, defensive)
		if byID[e.Src].Type != types.NodeModifier {
			continue
		}
		ovrs = append(ovrs, ovr{
			child:  byID[e.Src].QualifiedName,
			parent: byID[e.Dst].QualifiedName,
		})
	}
	sort.Slice(ovrs, func(i, j int) bool {
		if ovrs[i].child != ovrs[j].child {
			return ovrs[i].child < ovrs[j].child
		}
		return ovrs[i].parent < ovrs[j].parent
	})
	wantOvr := []ovr{
		{child: "Child.checkAmount", parent: "Base.checkAmount"},
		{child: "Child.onlyOwner", parent: "Base.onlyOwner"},
	}
	if len(ovrs) != len(wantOvr) {
		t.Errorf("W7.3 EdgeOverrides (modifier) count: got %d, want %d\n got=%v\nwant=%v",
			len(ovrs), len(wantOvr), ovrs, wantOvr)
	} else {
		for i := range wantOvr {
			if ovrs[i] != wantOvr[i] {
				t.Errorf("W7.3 EdgeOverrides[%d]: got %+v, want %+v", i, ovrs[i], wantOvr[i])
			}
		}
	}
}
