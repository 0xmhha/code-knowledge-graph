package solidity_test

import (
	"sort"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W7.4 V25 — explicit-list modifier override lockdown.
//
// W7.3 V0 (modifier_composition_test.go) locked the *bare* override
// form: `modifier m() override { _; }`. The sibling explicit-list
// form `modifier m() override(A, B) { _; }` has its own code path in
// runModifierOverride (overrides.go) — the
// dispatchKindOverrideExplicit branch — that emits one
// EdgeOverrides PendingRef per listed parent. That branch shipped
// without a lockdown test, which is exactly the silent-regression
// shape WALKER_SYMMETRY.md (internal/parse/solidity/) catalogues as
// drift class #1: a walker invariant added on one path with the
// sibling path implicitly assumed to track.
//
// V25 pins the explicit-list path. The fixture diamond-inherits from
// two unrelated abstract parents (A, B), each declaring the same
// virtual modifier name. The child must resolve the conflict with
// `override(A, B)` — Solidity rejects bare `override` when two
// unrelated bases declare the same virtual member.
//
// Expected outcome (0-line walker change required):
//
//   - Exactly two EdgeOverrides edges out of `Child.m`:
//     Child.m → A.m, Child.m → B.m.
//   - No spurious EdgeOverrides edges from anywhere else.
//   - Both source and destination of each edge are NodeModifier
//     (not NodeFunction) — the W2 function-pair resolver and the
//     W7.3 modifier-pair resolver share funcByQName but disambiguate
//     by NodeType.
//
// What this catches that V0 alone doesn't:
//
//	A refactor that drops or short-circuits the
//	`len(explicitParents) > 0` branch in runModifierOverride would
//	pass the bare-override fixture (which goes through the
//	dispatchKindOverride branch only) and silently lose the multi-
//	parent override edges. The bare path covers 80% of real-world
//	override usage; the explicit-list path covers the remaining 20%
//	(diamond / multi-base) — losing it makes ckg silently incorrect
//	for diamond contracts.
func TestModifierExplicitOverride_DiamondParents(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/modifier_composition",
		"explicit_override.sol")

	byID := map[string]types.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}

	type ovr struct{ child, parent string }
	var got []ovr
	for _, e := range edges {
		if e.Type != types.EdgeOverrides {
			continue
		}
		// modifier-pair only — defensive in case a future change
		// emits function overrides for the same fixture.
		if byID[e.Src].Type != types.NodeModifier {
			continue
		}
		if byID[e.Dst].Type != types.NodeModifier {
			t.Errorf("EdgeOverrides dst NodeType: src=%s dst=%s got %s, want NodeModifier",
				byID[e.Src].QualifiedName, byID[e.Dst].QualifiedName, byID[e.Dst].Type)
			continue
		}
		got = append(got, ovr{
			child:  byID[e.Src].QualifiedName,
			parent: byID[e.Dst].QualifiedName,
		})
	}
	sort.Slice(got, func(i, j int) bool {
		if got[i].child != got[j].child {
			return got[i].child < got[j].child
		}
		return got[i].parent < got[j].parent
	})

	want := []ovr{
		{child: "Child.m", parent: "A.m"},
		{child: "Child.m", parent: "B.m"},
	}
	if len(got) != len(want) {
		t.Errorf("EdgeOverrides (modifier) count: got %d, want %d\n got=%v\nwant=%v",
			len(got), len(want), got, want)
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("EdgeOverrides[%d]: got %+v, want %+v", i, got[i], want[i])
		}
	}
}
