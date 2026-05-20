package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W9 V4 — fixed-size value-type array slot packing. Extends W9 V2
// (which conservatively treated arrays as full slots) to actually
// compute the array's storage footprint and place it across as many
// slots as needed. Per Sol §11.1, fixed arrays start a new slot and
// the next variable also starts fresh, but their elements pack
// tightly inside their own slots.
func TestStorageSlot_FixedArrayPacking(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/storage_slot_packing", "array_packed.sol")

	want := map[string]int{
		"ArrayPacked.a": 0, // uint8[4] -> 1 slot
		"ArrayPacked.b": 1, // new slot after array
		"ArrayPacked.c": 2, // uint16[16] -> 32 bytes exactly
		"ArrayPacked.d": 3,
		"ArrayPacked.e": 4, // uint8[33] -> 2 slots (4, 5)
		"ArrayPacked.f": 6,
		"ArrayPacked.g": 7, // uint256[2] -> 2 slots (7, 8)
		"ArrayPacked.h": 9,
		"ArrayPacked.i": 10, // uint8[4][2] -> 8 bytes in 1 slot
		"ArrayPacked.j": 11,
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
