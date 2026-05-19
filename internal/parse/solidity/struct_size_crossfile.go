package solidity

import (
	"sort"
	"strings"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Sol W-C W9 V6 (2026-05-19) — cross-file struct size correction
// for storage slot index.
//
// computeStructSizes (Pass 1) sizes structs the file owns. Pass 2
// receives a global structSizes table merged across every parsed
// file (Parser.structSizes guarded by structMu), so a state
// variable typed as a foreign struct can finally be sized
// correctly. This helper walks every contract's state variables in
// declaration order and re-runs the slotState packing logic with
// the global table, then writes the resulting absolute slot index
// (within the contract — inheritance offset is added separately
// by applyInheritanceSlotOffset).
//
// V6 doesn't touch the inheritance offset side of slot computation
// (still in W9 V1 territory); diamond C3 linearization remains
// deferred. The helper runs BEFORE applyInheritanceSlotOffset so
// the contract-local correction lands before the inheritance offset
// accumulates on top.

func applyCrossFileStructSizes(nodes []types.Node, globalStructSizes map[string]int) {
	if len(globalStructSizes) == 0 || len(nodes) == 0 {
		return
	}
	// Group field / mapping nodes by their enclosing contract name
	// (parsed from QualifiedName). Sort each group by StartByte so
	// the slotState pass walks declarations in source order.
	type slotNode struct {
		idx       int
		startByte int
		signature string
		typeKind  types.NodeType
		isFnTyped bool
	}
	byContract := map[string][]slotNode{}
	for i, n := range nodes {
		if n.Type != types.NodeField && n.Type != types.NodeMapping {
			continue
		}
		dot := strings.IndexByte(n.QualifiedName, '.')
		if dot <= 0 || dot == len(n.QualifiedName)-1 {
			continue
		}
		contract := n.QualifiedName[:dot]
		byContract[contract] = append(byContract[contract], slotNode{
			idx:       i,
			startByte: n.StartByte,
			signature: n.Signature,
			typeKind:  n.Type,
			isFnTyped: n.IsFunctionTyped,
		})
	}
	for _, group := range byContract {
		sort.Slice(group, func(a, b int) bool {
			return group[a].startByte < group[b].startByte
		})
		state := slotState{}
		for _, sn := range group {
			var slot int
			switch {
			case sn.typeKind == types.NodeMapping:
				slot, state = advanceForMapping(state)
			case sn.isFnTyped:
				// Function-typed state vars consume one full slot
				// (Sol stores the (selector, address) pair in 24
				// bytes; layout-wise they take the slot).
				slot, state = advanceForField(state, 32)
			default:
				if arrayBytes, ok := solFixedArrayBytes(sn.signature); ok {
					slot, state = advanceForArrayField(state, arrayBytes)
				} else if structBytes, ok := globalStructSizes[sn.signature]; ok {
					slot, state = advanceForArrayField(state, structBytes)
				} else if size, valOk := solValueTypeSize(sn.signature); valOk {
					slot, state = advanceForField(state, size)
				} else {
					// Truly unknown type — fall back to the
					// conservative full-slot advance same as
					// runStateVarDecl's pre-V6 path.
					slot, state = advanceForField(state, 32)
				}
			}
			nodes[sn.idx].SlotIndex = slot
		}
	}
}
