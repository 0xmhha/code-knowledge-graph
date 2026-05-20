package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W9 V13 / V14 — enum slot occupancy. Sol enums with ≤256
// variants compile to uint8 (1 byte) at runtime, so consecutive
// enum fields pack with each other and with adjacent small
// primitives. V13 originally locked the conservative full-slot
// fallback (one slot per enum); V14 added a per-file enumSizes
// index so enum-typed state vars route through advanceForField
// with 1-byte sizing.
//
// Expected layout under V14:
//
//	head  -> slot 0, byte 0
//	role1 -> slot 0, byte 1 (uint8 enum)
//	role2 -> slot 0, byte 2 (uint8 enum)
//	tail  -> slot 1        (uint256 needs full slot)
func TestStorageSlot_EnumConservativePacking(t *testing.T) {
	nodes, _ := parseResolveOneSol(t,
		"testdata/storage_slot_packing", "enum_packing.sol")

	want := map[string]int{
		"EnumHolder.head":  0,
		"EnumHolder.role1": 0, // V14: packs with head
		"EnumHolder.role2": 0, // V14: packs with head + role1
		"EnumHolder.tail":  1, // uint256 advances to the next slot
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
