// W-C W6 V1.1 fixture — function-parameter receiver dispatch.
// Expectations:
//   - 1 EdgeUsesFor: Calc → Math    (V0 binding edge)
//   - 1 EdgeCalls: Calc.double → Math.times
//     (`x.times(2)` where x is a function parameter of type uint256)
//   - All EXTRACTED.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library Math {
    function times(uint256 a, uint256 b) internal pure returns (uint256) {
        return a * b;
    }
}

contract Calc {
    using Math for uint256;

    function double(uint256 x) external pure returns (uint256) {
        return x.times(2);
    }
}
