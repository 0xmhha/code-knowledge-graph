package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W9 V13 — enum slot occupancy audit. Sol enums with ≤256
// variants are stored as uint8 (1 byte) at runtime, so two
// consecutive enum fields could pack with each other and with
// adjacent small primitives. solTypeSize currently doesn't
// recognise user-defined enum names — they fall through the
// 32-byte conservative fallback, so each enum field occupies a
// full slot. This is a real packing miss but the fix needs a
// per-contract enum size index (W9 V14+ scope).
//
// V13 locks the conservative behaviour so a future change to
// the enum-packing path doesn't break silently. The expected
// layout under the current model:
//
//   head  -> slot 0
//   role1 -> slot 1 (would pack in V14+)
//   role2 -> slot 2 (would pack in V14+)
//   tail  -> slot 3
func TestStorageSlot_EnumConservativePacking(t *testing.T) {
	nodes, _ := parseResolveOneSol(t,
		"testdata/storage_slot_packing", "enum_packing.sol")

	want := map[string]int{
		"EnumHolder.head":  0,
		"EnumHolder.role1": 1,
		"EnumHolder.role2": 2,
		"EnumHolder.tail":  3,
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
