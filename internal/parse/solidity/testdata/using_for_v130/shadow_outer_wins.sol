// W-C W6 V1.30 fixture — block-scoped shadowing.
//
// Function f() declares outer `uint256 x = 1` followed by an inner
// block that shadows: `bytes32 x = bytes32(0)`. The outer use site
// `x.add(1)` (return statement) is in the outer scope and must
// dispatch via uint256 → SafeMath.add.
//
// Pre-V1.30 last-decl-wins: V1.15's pre-build sweep overwrites
// localVarTypes[f]["x"] in tree-sitter source order. Inner block's
// `bytes32 x` is encountered second (depth-first descent), so the
// map ends with x → bytes32 → no SafeMath binding → outer use site
// drops (false-negative).
//
// V1.30 V0 "first-decl wins": Pass 2 sweep only writes the first
// emitted (varName, typeName) per (funcID, varName). Outer `uint256 x`
// is emitted before inner shadow (tree-sitter source order). The map
// ends with x → uint256 → SafeMath.add resolves.
//
// Trade-off: inner-block use sites where the shadow type would be
// the correct resolution still resolve to outer in V1.30 V0. Real-
// world Sol rarely uses inner shadowing with using-for receivers in
// the inner block — V2+ refactor would use byte-range-aware lookup
// for full correctness.
//
// Expectations:
//   - 1 EdgeUsesFor: C → SafeMath
//   - 1 EdgeCalls (outer use site): C.f → SafeMath.add

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract C {
    using SafeMath for uint256;

    function f(bool cond) external pure returns (uint256) {
        uint256 x = 1;
        if (cond) {
            bytes32 x = bytes32(0); // inner shadow — bytes32 not bound
            x; // silence unused
        }
        return x.add(1); // outer use — must resolve via outer x = uint256
    }
}
