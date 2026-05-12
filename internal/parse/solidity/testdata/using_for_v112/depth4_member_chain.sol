// W-C W6 V1.12 fixture — depth-4 generic member chain. Confirms
// arbitrary depth (not just 3).
// Expectations:
//   - 1 EdgeUsesFor: Top → MoreLib
//   - 1 EdgeCalls (V1.12): Top.run → MoreLib.add

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library MoreLib {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

struct L4 {
    uint256 val;
}

struct L3 {
    L4 four;
}

struct L2 {
    L3 three;
}

struct L1 {
    L2 two;
}

contract Top {
    using MoreLib for uint256;

    L1 public root;

    function run() external view returns (uint256) {
        return root.two.three.four.val.add(1);
    }
}
