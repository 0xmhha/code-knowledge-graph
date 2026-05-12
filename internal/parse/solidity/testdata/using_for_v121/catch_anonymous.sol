// W-C W6 V1.21 baseline guard — catch_clause without parameters
// (`catch { ... }`) or with anonymous slot. V1.21 emit must skip
// silently when there's no parameter name. Over-reach 0.
//
// Expectations:
//   - 1 EdgeUsesFor: C → SafeMath
//   - 1 EdgeCalls (V1.20 try-returns r — UNRELATED to V1.21 path):
//     C.f → SafeMath.add (line for r.add in try body)
//     V1.21 catch path contributes 0 since no named catch param.

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
        try ext.compute() returns (uint256 r) {
            return r.add(1); // V1.20 try-returns path
        } catch {
            return 0; // anonymous catch — V1.21 emit skip
        }
    }
}
