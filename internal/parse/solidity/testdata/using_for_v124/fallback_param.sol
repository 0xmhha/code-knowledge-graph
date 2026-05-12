// W-C W6 V1.24 fixture — fallback(bytes calldata) with using-for receiver
// in body. fallback can carry parameters in Sol 0.6+. Here we use a body
// local of uint256 type (bound to SafeMath) as the dispatch receiver.
//
// Expectations:
//   - 1 EdgeUsesFor: C → SafeMath
//   - 1 EdgeCalls (V1.24 fallback local): C.fallback → SafeMath.add

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract C {
    using SafeMath for uint256;

    uint256 public counter;

    fallback() external payable {
        uint256 step = 1;
        counter = step.add(0);
    }
}
