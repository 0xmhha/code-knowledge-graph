// W-C W6 V1.23 fixture — constructor body local variable as using-for
// receiver. `constructor() { uint256 base = 10; stored = base.add(5); }`.
// Confirms V1.23 also routes constructor_definition bodies through
// emitLocalVarMetaPending.
//
// Expectations:
//   - 1 EdgeUsesFor: C → SafeMath
//   - 1 EdgeCalls (V1.23 constructor local): C.constructor → SafeMath.add

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract C {
    using SafeMath for uint256;

    uint256 public stored;

    constructor() {
        uint256 base = 10;
        stored = base.add(5);
    }
}
