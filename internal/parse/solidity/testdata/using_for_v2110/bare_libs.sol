// W-C W6 V2.11 fixture (library side). Same SafeMath library as
// V1.28 / V2.10 use; the caller-side uses the bare path-only import
// form (no curly-brace named entries, no alias, no namespace).

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}
