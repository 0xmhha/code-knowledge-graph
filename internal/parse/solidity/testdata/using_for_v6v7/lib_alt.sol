// W-C W6 V7 fixture — homonym SubMath library that the resolver
// must NOT pick. The consumer's namespace alias `M` points at
// lib.sol, not this file.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

library SubMath {
    function addOne(uint256 x) internal pure returns (uint256) {
        return x; // intentionally different impl
    }
}
