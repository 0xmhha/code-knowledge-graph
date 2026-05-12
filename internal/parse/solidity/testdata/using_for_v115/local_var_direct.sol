// W-C W6 V1.15 fixture — single-return local-var receiver (V1.0 cousin
// with local-var fallback). `uint256 x = expr; x.add(1)` — receiver is
// a function-local variable. localVarTypes index resolves x → uint256
// → SafeMath.
// Expectations:
//   - 1 EdgeUsesFor: Calculator → SafeMath
//   - 1 EdgeCalls (V1.15): Calculator.compute → SafeMath.add

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract Calculator {
    using SafeMath for uint256;

    function compute(uint256 seed) external pure returns (uint256) {
        uint256 x = seed * 2;
        return x.add(1);
    }
}
