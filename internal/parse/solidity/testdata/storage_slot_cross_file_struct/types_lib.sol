// W-C W9 V16 fixture (file 1/2) — external struct definition.
// Pair has two uint128 fields (16 bytes each, total 32 bytes —
// same-file V5 packing routes this through advanceForArrayField
// with size=32, occupying exactly one slot). The companion
// holder.sol file uses Pair as a state-var via namespace alias
// import.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

struct Pair {
    uint128 a;
    uint128 b;
}
