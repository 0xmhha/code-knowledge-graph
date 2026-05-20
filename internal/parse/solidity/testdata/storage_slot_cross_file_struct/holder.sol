// W-C W9 V16 fixture (file 2/2) — cross-file struct state-var.
//
// Holder imports types_lib.sol as the `Ext` namespace alias and
// declares Ext.Pair as one of its state-vars. The W9 V5 per-file
// structSizes index only knows structs declared in the same
// file, so `Ext.Pair` doesn't get the precise 32-byte size — it
// falls through to solTypeSize's conservative 32-byte fallback.
//
// In this particular case the V5 ideal (1 slot for Pair) and the
// conservative fallback (1 slot for an unknown 32-byte signature)
// happen to coincide because Pair is exactly one slot wide. The
// V16 audit fixture deliberately places a uint128 neighbour on
// the other side of pair to verify the slot-advance behaviour —
// pair currently advances slot1 because solTypeSize returns 32
// and advanceForArrayField pre-aligns to a fresh slot.
//
// Expected slot layout under V16 (audit-lock the gap):
//
//   head : uint128   slot 0, byte 0  (16 bytes consumed, 16 free)
//   pair : Ext.Pair  slot 1          (conservative full-slot
//                                     fallback forces a fresh slot
//                                     and skips the 16 free bytes
//                                     after head)
//   tail : uint256   slot 2
//
// V17+ would index globalStructSizes across all ParseResults and
// route this through advanceForArrayField with the actual sum-of-
// field-bytes (32 in this case), at which point the layout would
// match the same-file V5 outcome.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "./types_lib.sol" as Ext;

contract Holder {
    uint128    head;
    Ext.Pair   pair;
    uint256    tail;
}
