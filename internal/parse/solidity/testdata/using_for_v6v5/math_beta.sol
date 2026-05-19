// W-C W6 V5 fixture (homonym half B) — declares a second `mul`.
// The consumer never imports this file; the using-for resolver
// must NOT pick this candidate.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

function mul(uint256 a, uint256 b) pure returns (uint256) {
    return a + b; // intentionally wrong impl so a mistaken bind is obvious
}
