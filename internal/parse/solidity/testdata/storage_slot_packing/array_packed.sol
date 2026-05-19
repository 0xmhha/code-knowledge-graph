// W-C W9 V4 fixture — fixed-size value-type array packing.
//
// Sol §11.1 storage layout for fixed-size arrays:
//   - Array elements pack tightly within consecutive slots.
//   - Items following an array start a new slot (the leftover bytes
//     in the array's last slot are not reused).
//   - Arrays themselves start a new slot.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract ArrayPacked {
    uint8[4]  a;     // slot 0   — 4 bytes in 1 slot
    uint8     b;     // slot 1   — new slot after array
    uint16[16] c;    // slot 2   — 32 bytes exactly, 1 slot
    uint8     d;     // slot 3   — new slot after array
    uint8[33] e;     // slot 4-5 — 33 bytes -> 2 slots
    uint8     f;     // slot 6   — new slot after array
    uint256[2] g;    // slot 7-8 — 64 bytes -> 2 slots
    uint8     h;     // slot 9   — new slot after array
    uint8[4][2] i;   // slot 10  — 2 x (uint8[4]) = 8 bytes in 1 slot
    uint8     j;     // slot 11  — new slot after nested array
}
