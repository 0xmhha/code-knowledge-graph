// W-C W9 V2 fixture — type-size aware storage packing.
//
// Sol §11.1 packs consecutive sub-32-byte state vars into a single
// slot until adding the next field would exceed 32 bytes; any size
// ≥ 32 byte field starts a fresh slot. The packing here covers the
// primitive coverage of solTypeSize: bool, uintN, intN, address,
// bytesN. Dynamic types (bytes, string), arrays, structs, and
// mappings all conservatively consume a full slot.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Packed {
    uint8 a;          // slot 0, bytes 0..0   (1 used)
    uint8 b;          // slot 0, bytes 1..1   (2 used)
    uint16 c;         // slot 0, bytes 2..3   (4 used)
    uint256 d;        // slot 1, full         (advance from 0→1)
    bool e;           // slot 2, byte 0       (1 used)
    address f;        // slot 2, bytes 1..20  (21 used)
    bytes32 g;        // slot 3, full         (bytes32 == 32 bytes)
    int128 h;         // slot 4, bytes 0..15  (16 used)
    int128 i;         // slot 4, bytes 16..31 (32 used → advance)
    string j;         // slot 5, full         (dynamic, always full)
    bool k;           // slot 6, byte 0       (1 used)
}
