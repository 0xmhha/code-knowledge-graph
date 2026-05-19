package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W9 V5 — struct field aggregation for storage packing. Sums
// each struct's member byte footprint with the same packing rules as
// top-level state vars, then uses the total to size NodeField rows
// whose user_defined_type matches a known struct. Pre/post-slot
// alignment follows Sol §11.1's "items following a struct start a
// new slot" rule.
func TestStorageSlot_StructPacking(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/storage_slot_packing", "struct_packed.sol")

	// Layout walkthrough:
	//   head  : uint8        slot 0
	//   innerVar : Inner     slot 1 (struct starts new slot, occupies 1..2)
	//   middle : uint8       slot 3 (after struct, new slot)
	//   outerVar : Outer     slot 4 (struct starts new slot, occupies 4..6)
	//   tail : uint8         slot 7
	want := map[string]int{
		"StructPacked.head":     0,
		"StructPacked.innerVar": 1,
		"StructPacked.middle":   3,
		"StructPacked.outerVar": 4,
		"StructPacked.tail":     7,
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
