// W-C W6 V1.24 fixture — receive() with using-for receiver in body.
// `receive() external payable { uint256 v = msg.value; v.add(1); }` —
// receive() takes no parameters (Sol language rule) but can have body
// locals. Pre-V1.24, fallback_receive_definition had no graph node, so
// the local var `v` was never indexed.
//
// Expectations:
//   - 1 EdgeUsesFor: C → SafeMath
//   - 1 EdgeCalls (V1.24 receive local): C.receive → SafeMath.add

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract C {
    using SafeMath for uint256;

    uint256 public total;

    receive() external payable {
        uint256 v = msg.value;
        total = v.add(1);
    }
}
