// W-C W9 V11 fixture — 3-level deep nested struct packing.
//
// Sol §11.1 struct layout:
//   - struct members pack the same way as top-level state vars
//   - the struct itself begins on a slot boundary
//   - the variable after a struct also starts on a fresh slot
//
// Inner layout: 1B + 1B + 32B aligned -> a/b share slot 0,
// c (uint256) takes slot 1. Total 2 slots = 64 bytes.
//
// Middle wraps Inner + one uint256:
//   inner : 2 slots (slot 0-1)
//   x     : 1 slot  (slot 2)
//   total: 3 slots = 96 bytes
//
// Outer wraps Middle + one uint8:
//   middle: 3 slots (slot 0-2)
//   y     : 1 slot  (slot 3 — fresh after struct)
//   total: 4 slots = 128 bytes
//
// State variables:
//   head       : uint8         slot 0
//   wrapped    : Outer         slot 1-4  (4 slots)
//   tail       : uint8         slot 5    (new slot after struct)
//
// V11 asserts the fixed-point loop in computeStructSizes
// resolves Inner -> Middle -> Outer in increasing dependency
// order and the final SlotIndex on each state var matches the
// hand-walked layout.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract DeeplyNested {
    struct Inner {
        uint8   a;
        uint8   b;
        uint256 c;
    }

    struct Middle {
        Inner   inner;
        uint256 x;
    }

    struct Outer {
        Middle  middle;
        uint8   y;
    }

    uint8 head;
    Outer wrapped;
    uint8 tail;
}
