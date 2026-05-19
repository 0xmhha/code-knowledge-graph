// W-C W9 V9 fixture (with using directives) — exercises three
// using shapes alongside the same state variables as the baseline
// contract. Sol §11.1 explicitly says using directives don't
// participate in storage layout, so the SlotIndex on every field
// must match the no-using equivalent below.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

library Adder {
    function addOne(uint256 x) internal pure returns (uint256) { return x + 1; }
}

function mulOne(uint256 x, uint256 y) pure returns (uint256) { return x * y + 1; }

contract WithUsing {
    using Adder for uint256;
    using {mulOne as *} for uint256;

    uint8   a;
    uint8   b;
    uint16  c;
    uint256 d;
    address e;
}
