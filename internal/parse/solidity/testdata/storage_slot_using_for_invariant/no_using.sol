// W-C W9 V9 fixture (baseline) — identical state variables to
// with_using.sol but without any using directives. The slot
// indices on the corresponding fields must match exactly.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

contract NoUsing {
    uint8   a;
    uint8   b;
    uint16  c;
    uint256 d;
    address e;
}
