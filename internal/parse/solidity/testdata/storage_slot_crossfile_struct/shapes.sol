// W-C W9 V6 fixture (definition half) — declares Inner struct used
// across files. Layout per Sol §11.1:
//   a (uint8)  slot 0 used 1
//   b (uint8)  slot 0 used 2
//   c (uint16) slot 0 used 4
//   d (uint256) slot 1
// total 2 slots = 64 bytes.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

struct Inner {
    uint8   a;
    uint8   b;
    uint16  c;
    uint256 d;
}
