// W-C W6 V1.14 fixture (cross-file, lib + structs side).
// Library SafeMath + struct UserData / Org declared here. Used by
// cross_file_vault10.sol (V1.10 depth-1) and cross_file_vault13.sol
// (V1.13 this-prefixed depth-2). Cross-file resolution → ConfInferred.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

struct UserData {
    uint256 balance;
}

struct Org {
    UserData user;
}
