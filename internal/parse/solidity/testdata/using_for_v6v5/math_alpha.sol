// W-C W6 V5 fixture (homonym half A) — declares free function `mul`.
// A second file (math_beta.sol) declares another free function with
// the same name. The consumer file imports only this file under the
// namespace alias `M`, so a using-for directive `using {M.mul as *}`
// must resolve to THIS file's mul, not the homonym in math_beta.sol.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

function mul(uint256 a, uint256 b) pure returns (uint256) {
    return a * b;
}
