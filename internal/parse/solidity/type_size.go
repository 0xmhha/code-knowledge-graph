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
//	bool               → 1
//	address, address payable → 20
//	uintN  (N ∈ 8..256, N % 8 == 0) → N/8
//	intN   (N ∈ 8..256, N % 8 == 0) → N/8
//	uint, int          → 32 (aliases for uint256 / int256)
//	bytesN (N ∈ 1..32) → N
//
// Defaults to 32 (full slot, conservative) for:
//
//	bytes (dynamic) / string / arrays / structs / mappings /
//	user-defined types / anything not in the above list.
func solTypeSize(sig string) int {
	if size, ok := solValueTypeSize(sig); ok {
		return size
	}
	return 32
}

// solValueTypeSize returns the byte footprint of a Sol value type
// signature along with a flag indicating whether the signature is
// in the recognised value-type set. Callers needing to distinguish
// "known full-slot type" (uint256, bytes32, int256) from "unknown
// conservative fallback" (string, custom struct, ...) read the
// second return.
func solValueTypeSize(sig string) (int, bool) {
	sig = strings.TrimSpace(sig)
	switch sig {
	case "bool":
		return 1, true
	case "address", "address payable":
		return 20, true
	case "uint", "int":
		return 32, true
	}
	if strings.HasPrefix(sig, "uint") {
		if rest := sig[len("uint"):]; rest != "" {
			n, err := strconv.Atoi(rest)
			if err == nil && n >= 8 && n <= 256 && n%8 == 0 {
				return n / 8, true
			}
		}
	}
	if strings.HasPrefix(sig, "int") {
		if rest := sig[len("int"):]; rest != "" {
			n, err := strconv.Atoi(rest)
			if err == nil && n >= 8 && n <= 256 && n%8 == 0 {
				return n / 8, true
			}
		}
	}
	if strings.HasPrefix(sig, "bytes") {
		if rest := sig[len("bytes"):]; rest != "" {
			n, err := strconv.Atoi(rest)
			if err == nil && n >= 1 && n <= 32 {
				return n, true
			}
		}
	}
	return 0, false
}

// solFixedArrayBytes parses a Sol fixed-size value-type array
// signature (`T[N]`, `T[N][M]`, …) and returns the total byte
// footprint = element_size * N * M * … . Returns ok=false for
// dynamic arrays (`T[]`), non-array signatures, and arrays whose
// element type isn't in the recognised value-type set (callers fall
// back to the conservative full-slot path).
//
// Sol §11.1: fixed-size value-type arrays pack their elements
// tightly into consecutive storage slots. Compound shapes nest:
// `uint8[4][2]` is two arrays of `uint8[4]`, so the total is
// 2 × 4 × 1 = 8 bytes. Elements span slot boundaries — a 33-byte
// array (`uint8[33]`) consumes two slots with one byte spilling
// into the second.
func solFixedArrayBytes(sig string) (int, bool) {
	sig = strings.TrimSpace(sig)
	if !strings.HasSuffix(sig, "]") {
		return 0, false
	}
	openIdx := strings.LastIndex(sig, "[")
	if openIdx < 0 {
		return 0, false
	}
	closeIdx := len(sig) - 1
	countStr := strings.TrimSpace(sig[openIdx+1 : closeIdx])
	if countStr == "" {
		return 0, false
	}
	n, err := strconv.Atoi(countStr)
	if err != nil || n <= 0 {
		return 0, false
	}
	inner := strings.TrimSpace(sig[:openIdx])
	var elementSize int
	if strings.HasSuffix(inner, "]") {
		innerBytes, ok := solFixedArrayBytes(inner)
		if !ok {
			return 0, false
		}
		elementSize = innerBytes
	} else {
		size, ok := solValueTypeSize(inner)
		if !ok {
			return 0, false
		}
		elementSize = size
	}
	return elementSize * n, true
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

// advanceForArrayField (W-C W9 V4, 2026-05-19) places a fixed-size
// value-type array. Sol §11.1 says struct- and array-shaped state
// variables always start a new slot, and the next variable after
// them also starts a new slot — so we pre-align, occupy
// ceil(totalBytes / 32) slots, and post-align by clearing used. The
// returned slot index is the first slot of the array.
func advanceForArrayField(state slotState, totalBytes int) (int, slotState) {
	if state.used > 0 {
		state.slot++
		state.used = 0
	}
	startSlot := state.slot
	slotsNeeded := max((totalBytes+31)/32, 1)
	state.slot += slotsNeeded
	state.used = 0
	return startSlot, state
}
