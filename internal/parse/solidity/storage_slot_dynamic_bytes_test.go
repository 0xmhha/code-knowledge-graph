package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W9 V12 — dynamic bytes/string slot occupancy audit. Both
// dynamic types occupy exactly one storage slot at the
// declaration site (length+content inline for short values,
// length-in-slot + hash-addressed content for long values).
// Either way the slot index calculation treats them as a single
// full slot, so the SlotIndex on every state variable matches
// the no-dynamic baseline plus one slot per dynamic-type field.
func TestStorageSlot_DynamicBytesStringPacking(t *testing.T) {
	nodes, _ := parseResolveOneSol(t,
		"testdata/storage_slot_packing", "dynamic_bytes_string.sol")

	want := map[string]int{
		"DynamicBytes.head":    0,
		"DynamicBytes.name":    1, // string — one full slot
		"DynamicBytes.payload": 2, // bytes  — one full slot
		"DynamicBytes.value":   3,
		"DynamicBytes.suffix":  4,
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
