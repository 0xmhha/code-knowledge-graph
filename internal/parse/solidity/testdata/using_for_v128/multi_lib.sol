// W-C W6 V1.28 fixture (multi-library side). Two libraries defined,
// both imported under aliases in multi_caller.sol.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

library Address {
    function isZero(address a) internal pure returns (bool) {
        return a == address(0);
    }
}
