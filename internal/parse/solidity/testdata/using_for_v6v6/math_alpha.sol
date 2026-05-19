// W-C W6 V6 fixture (homonym half A) — exports the free function
// `origMul`. The consumer imports it under the named-import alias
// `mul`, and a homonym `mul` lives in math_beta.sol.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

function origMul(uint256 a, uint256 b) pure returns (uint256) {
    return a * b;
}
