// W-C W6 V2.10 fixture (library side). Two libraries defined; the
// caller imports one bare (`SafeMath`) and one aliased (`Address as A`)
// in a single import statement.

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
