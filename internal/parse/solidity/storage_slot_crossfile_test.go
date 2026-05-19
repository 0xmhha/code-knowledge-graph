package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W9 V6 — cross-file struct sizing. ParseFile runs per-file
// Pass 1 so a state variable typed as a struct defined in another
// file pre-V6 fell through to the conservative 32-byte advance.
// Resolve now merges every parser instance's struct size map
// (Parser.structSizes guarded by structMu) and re-packs each
// contract's state variables with the global table, then
// inheritance offset accumulation runs on top.
func TestStorageSlot_CrossFileStructPacking(t *testing.T) {
	nodes, _ := parseResolveMultiSol(t, "testdata/storage_slot_crossfile_struct",
		[]string{"shapes.sol", "holder.sol"})

	// Inner = 2 slots (64 bytes). Layout walkthrough:
	//   head   : uint8   slot 0
	//   inner  : Inner   slot 1 (struct -> 2 slots)
	//   middle : uint8   slot 3 (after struct, new slot)
	//   second : Inner   slot 4 (struct -> 2 slots)
	//   tail   : uint8   slot 6
	want := map[string]int{
		"Holder.head":   0,
		"Holder.inner":  1,
		"Holder.middle": 3,
		"Holder.second": 4,
		"Holder.tail":   6,
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
