// W-C W9 V5 fixture — struct field aggregation for storage packing.
//
// Sol §11.1: storage struct fields pack tightly inside the struct,
// same rules as top-level state vars. The struct itself begins on a
// slot boundary and the next variable after it also starts fresh.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract StructPacked {
    // Inner: 1+1+1+32 = padded to 64 -> 2 slots (slot 0,1).
    // Member layout inside Inner:
    //   a (uint8)  slot 0 used 1
    //   b (uint8)  slot 0 used 2
    //   c (uint16) slot 0 used 4
    //   d (uint256) slot 1
    struct Inner {
        uint8   a;
        uint8   b;
        uint16  c;
        uint256 d;
    }

    // Outer: contains Inner (2 slots) plus a uint8 e (1 byte).
    //   Inner inner             -> 2 slots (0,1)
    //   uint8  e   (after struct, new slot) -> slot 2 used 1
    // total occupied slots (rounded up): 3 (last partial slot counted)
    struct Outer {
        Inner inner;
        uint8 e;
    }

    uint8  head;     // slot 0  used 1
    Inner  innerVar; // slot 1..2  (struct starts new slot)
    uint8  middle;   // slot 3  (after struct, new slot)
    Outer  outerVar; // slot 4..6  (3 slots)
    uint8  tail;     // slot 7
}
