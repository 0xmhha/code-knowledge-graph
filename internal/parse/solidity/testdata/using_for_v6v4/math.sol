// W-C W6 V4 fixture (module half) — exports the free function `mul`
// that the consumer file imports under a namespace alias.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

function mul(uint256 a, uint256 b) pure returns (uint256) {
    return a * b;
}
