// W-C W9 V10 fixture (with free-function using directives) —
// exercises Sol 0.8.13+ `using {f} for T;` (no operator). Same
// state variables as the baseline contract; Sol §11.1 says no
// using directive consumes storage, so the SlotIndex on every
// field must match the no-using equivalent.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

library Adder {
    function addOne(uint256 x) internal pure returns (uint256) { return x + 1; }
}

contract WithUsingFreeFn {
    using {Adder.addOne} for uint256;

    uint8   a;
    uint8   b;
    uint16  c;
    uint256 d;
    address e;
}
