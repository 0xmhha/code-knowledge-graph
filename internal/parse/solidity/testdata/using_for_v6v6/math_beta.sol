// W-C W6 V6 fixture (homonym half B) — declares a free function
// also named `origMul` (different file, same name). The consumer
// never imports this file; the resolver must NOT pick this
// candidate.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

function origMul(uint256 a, uint256 b) pure returns (uint256) {
    return a + b; // intentionally wrong impl
}
