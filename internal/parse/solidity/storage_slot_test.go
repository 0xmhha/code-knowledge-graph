package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W9 V0 / V2 — per-contract storage slot index for NodeField.
//
// V0 emitted one slot per state-var. V2 (2026-05-18) added type-size
// aware packing so consecutive sub-32-byte primitives share a slot
// per Sol §11.1 layout rules. The Layout fixture below mixes a
// uint256, an address (20 bytes), a uint8 (1 byte), a mapping, and
// a bool to exercise packing:
//
//   uint256 totalSupply (slot 0, 32B full)
//   address owner       (slot 1, 20B used)
//   uint8   decimals    (slot 1, 21B used — packed with owner)
//   mapping balances    (slot 2, full — mapping always one full slot)
//   bool    paused      (slot 3, 1B used)
//
// Other contract still restarts at slot 0 (no inheritance).

func TestStorageSlot_PerContractIndex(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/storage_slot", "contract_layout.sol")

	want := map[string]int{
		"Layout.totalSupply": 0,
		"Layout.owner":       1,
		"Layout.decimals":    1, // V2: packs with owner (20 + 1 = 21 bytes)
		"Layout.paused":      3, // mapping consumes slot 2
		"Other.a":            0,
		"Other.b":            1,
	}

	got := map[string]int{}
	seen := map[string]bool{}
	for _, n := range nodes {
		if n.Type != types.NodeField {
			continue
		}
		if _, ok := want[n.QualifiedName]; ok {
			got[n.QualifiedName] = n.SlotIndex
			seen[n.QualifiedName] = true
		}
	}

	for qn, wantSlot := range want {
		if !seen[qn] {
			t.Errorf("W9 missing NodeField %q", qn)
			continue
		}
		if got[qn] != wantSlot {
			t.Errorf("W9 NodeField %q SlotIndex: got %d, want %d", qn, got[qn], wantSlot)
		}
	}

	// W9 V3 (2026-05-18): mappings now carry SlotIndex too. The Layout
	// fixture has `mapping balances` between decimals (slot 1, packed
	// with owner) and paused (slot 3), so balances occupies slot 2.
	// Per-key data still lives at keccak256(key, slot=2) at runtime.
	for _, n := range nodes {
		if n.QualifiedName == "balances:mapping" || n.Name == "balances" {
			if n.Type == types.NodeMapping && n.SlotIndex != 2 {
				t.Errorf("W9 V3 NodeMapping %q SlotIndex: got %d, want 2",
					n.QualifiedName, n.SlotIndex)
			}
		}
	}
}
