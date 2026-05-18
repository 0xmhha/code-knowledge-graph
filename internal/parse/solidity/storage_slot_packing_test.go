package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W9 V2 — type-size aware storage packing for primitive Sol
// types. V0 emitted one slot per field; V2 packs consecutive
// sub-32-byte primitives into shared slots per Sol §11.1.
//
// Coverage (solTypeSize lookup table):
//   bool           → 1
//   address        → 20
//   uintN  (N/8)   → 1..32
//   intN   (N/8)   → 1..32
//   bytesN         → 1..32
//   dynamic bytes  → 32 (full slot)
//   string         → 32 (full slot)
//   anything else  → 32 (conservative)
//
// V2 limitations:
//   - Fixed-size arrays (uint8[4]) are conservatively a full slot
//     in V2; Sol's real layout packs short arrays. A V3+ extension
//     could parse the bracket size out of the signature.
//   - Struct fields are conservatively a full slot. Sol packs
//     struct members the same way as top-level state vars, but
//     V2 lacks struct-member size aggregation.

func TestStorageSlot_TypeSizePacking(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/storage_slot_packing", "packed.sol")

	want := map[string]int{
		"Packed.a": 0, // uint8  → slot 0, 1B used
		"Packed.b": 0, // uint8  → slot 0, 2B used
		"Packed.c": 0, // uint16 → slot 0, 4B used
		"Packed.d": 1, // uint256 → slot 1, full
		"Packed.e": 2, // bool   → slot 2, 1B used
		"Packed.f": 2, // address → slot 2, 21B used
		"Packed.g": 3, // bytes32 → slot 3, full
		"Packed.h": 4, // int128 → slot 4, 16B used
		"Packed.i": 4, // int128 → slot 4, 32B used → advance
		"Packed.j": 5, // string → slot 5, full
		"Packed.k": 6, // bool   → slot 6, 1B used
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
			t.Errorf("W9 V2 missing NodeField %q", qn)
			continue
		}
		if g != w {
			t.Errorf("W9 V2 NodeField %q SlotIndex: got %d, want %d", qn, g, w)
		}
	}
}
