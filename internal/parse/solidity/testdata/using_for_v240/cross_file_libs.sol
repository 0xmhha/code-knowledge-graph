// W-C W6 V2.4 fixture (libs side). LibA + LibB defined here, both
// have method on uint256 but disjoint sets — caller uses both via
// V2.2 multi-binding.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library LibA {
    function tag(uint256 self) internal pure returns (uint256) {
        return self;
    }
}

library LibB {
    function bump(uint256 self) internal pure returns (uint256) {
        return self + 1;
    }
}
