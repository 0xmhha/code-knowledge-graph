package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W9 V1 — inheritance offset on NodeField.SlotIndex.
//
// V0 produced per-contract local indices (each contract restarted at
// slot 0). V1 walks the EdgeExtends adjacency in Pass 2 and adds
// the cumulative ancestor slot count so the SlotIndex reflects the
// absolute EVM storage position the field occupies.
//
// V1 scope:
//   - Linear inheritance chains. Each child's offset = sum of all
//     transitive parents' slot counts.
//   - Diamond inheritance with repeated ancestors falls back to a
//     naive sum (no C3 linearization); the test below covers the
//     linear case only.
//   - Bit-packing remains out of scope (each state-var still
//     consumes a full slot in V1). Sol's actual layout merges
//     sub-32-byte types into one slot; a future V2+ will model
//     that with a per-primitive size table.

func TestStorageSlot_InheritanceOffset(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/storage_slot_inheritance", "linear_chain.sol")

	want := map[string]int{
		"A.a": 0,
		"A.b": 1,
		"B.c": 2, // A's 2 slots + own 0 = 2
		"C.d": 3, // A's 2 + B's 1 + own 0 = 3
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
			t.Errorf("W9 V1 missing NodeField %q", qn)
			continue
		}
		if g != w {
			t.Errorf("W9 V1 NodeField %q SlotIndex: got %d, want %d", qn, g, w)
		}
	}
}

// TestStorageSlot_InconsistentMROFallback — W-C W9 V8. When the
// inheritance graph has no consistent C3 linearization (A's MRO
// puts X before Y, B's MRO puts Y before X, Z inherits A and B),
// solc would reject the hierarchy. The parser falls back to
// depth-first ancestry and stamps HasInheritanceMROFallback on
// the offending node so downstream tooling surfaces the
// diagnostic.
func TestStorageSlot_InconsistentMROFallback(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/storage_slot_inheritance", "inconsistent_mro.sol")

	want := map[string]bool{
		"Z":     true,  // diamond with conflicting order -> fallback
		"A":     false, // consistent X,Y order
		"B":     false, // consistent Y,X order (still self-consistent)
		"X":     false,
		"Y":     false,
		"Plain": false, // single-parent inheritance, fine
	}
	got := map[string]bool{}
	for _, n := range nodes {
		if n.Type != types.NodeContract {
			continue
		}
		if _, ok := want[n.Name]; ok {
			got[n.Name] = n.HasInheritanceMROFallback
		}
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("HasInheritanceMROFallback on %q: got %v want %v", name, got[name], w)
		}
	}
}

// TestStorageSlot_DiamondC3Linearization — W-C W9 V7. Verifies that
// diamond inheritance (D extends B, C where both extend Base) does
// not double-count Base's slot. The C3 MRO for D is
// [D, B, C, Base]; the offset sums each ancestor once.
//
// Slot expectations (one uint256 per contract, no packing):
//
//	Base.x : slot 0  (offset 0)
//	B.y    : slot 1  (offset = Base.1)
//	C.z    : slot 1  (offset = Base.1)
//	D.w    : slot 3  (offset = B.1 + C.1 + Base.1)
//
// Pre-V7 D.w would have been slot 4 because the naive parents
// sum walked Base twice (once via B, once via C).
func TestStorageSlot_DiamondC3Linearization(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/storage_slot_inheritance", "diamond_c3.sol")

	want := map[string]int{
		"Base.x": 0,
		"B.y":    1,
		"C.z":    1,
		"D.w":    3,
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
