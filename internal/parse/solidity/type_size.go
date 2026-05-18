package solidity

import (
	"strconv"
	"strings"
)

// Sol W-C W9 V2 (2026-05-18) — primitive type byte-size lookup for
// EVM storage packing.
//
// Per Sol §11.1 storage layout: consecutive state variables whose
// combined size fits a single 32-byte slot share it. Dynamic and
// reference types (string, bytes, mapping, dynamic array, struct,
// user-defined) always start a new slot and consume the full 32
// bytes from the layout's perspective.
//
// solTypeSize is conservative: unrecognised signatures (custom user
// types, arrays of any size, structs) return 32 so packing doesn't
// accidentally pack them with primitives. The supported set is
// limited to the well-defined Sol value types whose size is
// expressible in bytes without inspecting type definitions.
//
// Supported (correct size returned):
//
//   bool               → 1
//   address, address payable → 20
//   uintN  (N ∈ 8..256, N % 8 == 0) → N/8
//   intN   (N ∈ 8..256, N % 8 == 0) → N/8
//   uint, int          → 32 (aliases for uint256 / int256)
//   bytesN (N ∈ 1..32) → N
//
// Defaults to 32 (full slot, conservative) for:
//   bytes (dynamic) / string / arrays / structs / mappings /
//   user-defined types / anything not in the above list.
func solTypeSize(sig string) int {
	sig = strings.TrimSpace(sig)
	switch sig {
	case "bool":
		return 1
	case "address", "address payable":
		return 20
	case "uint", "int":
		return 32
	}
	// uintN
	if strings.HasPrefix(sig, "uint") {
		if rest := sig[len("uint"):]; rest != "" {
			n, err := strconv.Atoi(rest)
			if err == nil && n >= 8 && n <= 256 && n%8 == 0 {
				return n / 8
			}
		}
	}
	// intN
	if strings.HasPrefix(sig, "int") {
		if rest := sig[len("int"):]; rest != "" {
			n, err := strconv.Atoi(rest)
			if err == nil && n >= 8 && n <= 256 && n%8 == 0 {
				return n / 8
			}
		}
	}
	// bytesN (fixed). Exclude bare "bytes" (dynamic) and "bytes32" etc.
	if strings.HasPrefix(sig, "bytes") {
		if rest := sig[len("bytes"):]; rest != "" {
			n, err := strconv.Atoi(rest)
			if err == nil && n >= 1 && n <= 32 {
				return n
			}
		}
	}
	return 32
}

// slotState carries the per-contract packing counter used by
// runStateVarDecl. `slot` is the current 0-indexed slot; `used` is
// the byte offset within the slot already consumed (0..32).
type slotState struct {
	slot int
	used int
}

// advanceForField returns the slot index this field occupies and
// the updated state. The field is sized via solTypeSize.
//
// Packing rules (per Sol §11.1):
//
//   - A size >= 32 field always starts on a slot boundary and fills
//     it entirely (advance state.slot afterwards, reset used).
//
//   - A smaller field shares the current slot if there's room
//     (state.used + size <= 32); otherwise it advances to a fresh
//     slot. After placement, if the slot is now exactly full, the
//     next field starts on a new slot.
func advanceForField(state slotState, size int) (int, slotState) {
	// >= 32-byte types always sit on a slot boundary.
	if size >= 32 {
		if state.used > 0 {
			state.slot++
			state.used = 0
		}
		slot := state.slot
		state.slot++
		state.used = 0
		return slot, state
	}
	if state.used+size > 32 {
		state.slot++
		state.used = 0
	}
	slot := state.slot
	state.used += size
	if state.used >= 32 {
		state.slot++
		state.used = 0
	}
	return slot, state
}

// advanceForMapping consumes a full slot for a mapping state-var
// and returns the slot the mapping occupies. W-C W9 V3 (2026-05-18)
// extends the V2 advance to also produce a SlotIndex so NodeMapping
// rows can be located by storage slot the same way NodeField rows
// can. The per-key data still lives at keccak256(key, slot) at
// runtime; this slot is just the declaration slot.
func advanceForMapping(state slotState) (int, slotState) {
	if state.used > 0 {
		state.slot++
		state.used = 0
	}
	slot := state.slot
	state.slot++
	return slot, state
}
