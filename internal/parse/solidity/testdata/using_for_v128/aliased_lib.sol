// W-C W6 V1.28 fixture (library + alias caller). SafeMath defined here;
// caller file imports as alias SM and uses-for via the alias.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}
