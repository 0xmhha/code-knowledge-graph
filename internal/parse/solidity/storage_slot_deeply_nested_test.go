package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W9 V11 — deep struct packing audit. Inner is a 2-slot
// struct, Middle wraps Inner + uint256 -> 3 slots, Outer wraps
// Middle + uint8 -> 4 slots. The state-var declaration order:
//
//	head    : uint8       slot 0
//	wrapped : Outer       slot 1 (struct boundary -> 4 slots)
//	tail    : uint8       slot 5 (fresh slot after struct)
//
// computeStructSizes runs a fixed-point loop: Inner is sized
// first (no struct deps), then Middle (depends on Inner), then
// Outer (depends on Middle). V11 locks the final layout — a
// regression that breaks the dependency order would shift
// wrapped or tail by a slot.
func TestStorageSlot_DeeplyNestedStructPacking(t *testing.T) {
	nodes, _ := parseResolveOneSol(t,
		"testdata/storage_slot_packing", "struct_deeply_nested.sol")

	want := map[string]int{
		"DeeplyNested.head":    0,
		"DeeplyNested.wrapped": 1,
		"DeeplyNested.tail":    5,
	}

	got := map[string]int{}
	for _, n := range nodes {
		if n.Type != types.NodeField {
			continue
		}
		if _, ok := want[n.QualifiedName]; ok {
			got[n.QualifiedName] = n.SlotIndex
		}
	}

	for qn, w := range want {
		g, present := got[qn]
		if !present {
			t.Errorf("missing NodeField %q", qn)
			continue
		}
		if g != w {
			t.Errorf("NodeField %q SlotIndex: got %d, want %d", qn, g, w)
		}
	}
}
