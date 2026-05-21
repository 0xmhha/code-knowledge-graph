// W-C W9 V17 fixture probe — multi-inheritance storage slot order.
//
// Solidity's storage layout rule for `contract C is A, B`:
//   1. Apply C3 linearization to the inheritance graph.
//   2. Lay out storage from most-base to most-derived
//      (reverse MRO order).
//   3. Within each contract, fields keep declaration order.
//
// For `contract C is A, B` with no diamond:
//   C3 MRO = [C, B, A]
//   storage order = [A, B, C]
//   → A's fields get slots 0..N_A-1
//   → B's fields get slots N_A..N_A+N_B-1
//   → C's own fields get slots starting at N_A+N_B
//
// This fixture pins the canonical case (no diamond, no
// type-size packing — every field is uint256, 1 slot each):
//
//   A.a  → slot 0
//   B.b  → slot 1  (offset by A's 1 slot)
//   C.c  → slot 2  (offset by A+B = 2 slots)
//
// V1 already handles single-inheritance offset; the question
// is whether the multi-inheritance offset accumulates left-to-
// right correctly when there are TWO bases in the inheritance
// list.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract MultiBaseA {
    uint256 public a;
}

contract MultiBaseB {
    uint256 public b;
}

contract MultiDerived is MultiBaseA, MultiBaseB {
    uint256 public c;
}
