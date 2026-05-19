// W-C W9 V7 fixture — diamond inheritance via C3 linearization.
//
// Pre-V7 (naive parents-sum) double-counted Base because B and C
// each contribute Base's slot count to D. With C3 the
// linearization MRO is [D, B, C, Base], so D's offset sums Base
// once.
//
// Layout walk-through:
//   Base : { x }            -> slot 0
//   B is Base : { y }       -> y at slot 1 (offset = Base.1 = 1)
//   C is Base : { z }       -> z at slot 1 (offset = Base.1 = 1)
//   D is B, C : { w }       -> w at slot 3 (offset = B.1 + C.1 + Base.1)
//
// Pre-V7 D.w would have been slot 4 (double-counting Base).

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Base {
    uint256 x;
}

contract B is Base {
    uint256 y;
}

contract C is Base {
    uint256 z;
}

contract D is B, C {
    uint256 w;
}
