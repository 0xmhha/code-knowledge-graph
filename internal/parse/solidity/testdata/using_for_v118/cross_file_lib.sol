// W-C W6 V1.18 fixture (cross-file, lib + struct side). Mirrors V1.14
// layout: library SafeMath and struct UserData live here; the V1.16
// tuple-destructuring caller lives in cross_file_vault.sol.

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
