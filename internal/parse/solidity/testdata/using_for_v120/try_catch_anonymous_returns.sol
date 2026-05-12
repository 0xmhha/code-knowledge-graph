// W-C W6 V1.20 baseline guard — try/catch with anonymous returns slot.
// `try foo() returns (uint256) { ... }` — no name on the return slot.
// V1.20 emit skip (no addressable receiver). Confirms over-reach 0
// after the try_statement scope addition.
//
// Expectations:
//   - 1 EdgeUsesFor: C → SafeMath
//   - 0 EdgeCalls (no addressable receiver in the success block)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

interface IExternal {
    function compute() external pure returns (uint256);
}

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract C {
    using SafeMath for uint256;

    IExternal public ext;

    function f() external returns (uint256) {
        try ext.compute() returns (uint256) {
            return 1; // anonymous return slot — nothing to address
        } catch {
            return 0;
        }
    }
}
