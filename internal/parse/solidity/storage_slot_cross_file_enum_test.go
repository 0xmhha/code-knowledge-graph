package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W9 V15 — cross-file enum size propagation audit. W9 V14
// introduced a per-file enumSizes index that drives 1-byte
// packing for enum-typed state-vars. Cross-file enum references
// (`Ext.Status` after `import "./status_lib.sol" as Ext`) stay
// outside the per-file map and fall through to solTypeSize's
// conservative 32-byte fallback.
//
// V15 locks the current conservative behaviour with this audit
// test. Promoting to actual cross-file size propagation needs a
// globalEnumSizes index aggregated across ParseResults and a
// Pass-2 post-pass that updates SlotIndex retroactively — out of
// scope for V15.
//
// A future regression that mis-extends per-file enumSizes to
// match qualified names (e.g. by stripping the namespace prefix
// blindly) would silently change slot indices on cross-file
// holders and fail this lock.
func TestStorageSlot_CrossFileEnumConservative(t *testing.T) {
	nodes, _ := parseResolveMultiSol(t,
		"testdata/storage_slot_cross_file_enum",
		[]string{"status_lib.sol", "holder.sol"})

	want := map[string]int{
		"Holder.head":    0,
		"Holder.status1": 1, // V15 lock: cross-file enum → full slot
		"Holder.tail":    2, // uint256 advances to next slot
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
			t.Errorf("NodeField %q SlotIndex: got %d, want %d (V15 conservative lock)",
				qn, g, w)
		}
	}
}
