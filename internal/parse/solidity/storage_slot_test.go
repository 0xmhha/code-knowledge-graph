package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W9 V0 — per-contract storage slot index for NodeField.
//
// V0 scope:
//   - Sequential 0-indexed slot per state-var in declaration order.
//   - Mapping state-vars (NodeMapping) skip slot assignment — they
//     follow a separate emit path and their slot is derived via
//     keccak at runtime.
//   - No bit-packing (uint8 counts as a full slot in V0).
//   - No inheritance offset (each contract restarts at slot 0).
//
// V1+ (out of V0) will add: bit-packing, inheritance offset, mapping
// slot derivation.

func TestStorageSlot_PerContractIndex(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/storage_slot", "contract_layout.sol")

	want := map[string]int{
		"Layout.totalSupply": 0,
		"Layout.owner":       1,
		"Layout.decimals":    2,
		"Layout.paused":      3,
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

	// Mappings (Layout.balances) must NOT carry a SlotIndex — they're
	// NodeMapping, not NodeField, and even if accidentally indexed,
	// V0 deliberately skips the slot path. The check here is implicit
	// in the want map (no entry for balances).
	for _, n := range nodes {
		if n.QualifiedName == "balances:mapping" || n.Name == "balances" {
			if n.Type == types.NodeMapping && n.SlotIndex != 0 {
				t.Errorf("W9 NodeMapping %q must not carry SlotIndex (got %d)",
					n.QualifiedName, n.SlotIndex)
			}
		}
	}
}
