// W-C W6 V2.19 fixture — free-function form partial-recovery boundary.
//
// V2.6 locked the 2-entry shape (`using {Math.add, Math.sub} for T;`)
// as producing 1 incidental EdgeUsesFor through V0's type_alias query
// match. The V2.17 AST probe revealed that recovery is a fortuitous
// partial parse — `using_directive` carries an ERROR sibling for the
// braces plus a single `type_alias` child whose identifier is captured
// as @lib. Any grammar bump could shift this shape.
//
// V2.19 extends the surface: a 4-entry free-function directive
// `using {Math.add, Math.sub, Math.mul, Math.div} for T;`. If the
// partial-recovery shape is stable across entry counts, the result
// should still be exactly 1 EdgeUsesFor (Caller → Math), since V0
// dedupes by (src, dst) and all 4 entries reference the same library.
//
// If a future grammar bump makes the parse cleaner, this test will
// either keep the 1-edge result (if the grammar still surfaces one
// matchable identifier) or break — which is the signal to revisit
// V2.6 / V2.14 / V2.17 / V2.19 coordinated lock-flip.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.13;

library Math {
    function add(uint256 a, uint256 b) internal pure returns (uint256) { return a + b; }
    function sub(uint256 a, uint256 b) internal pure returns (uint256) { return a - b; }
    function mul(uint256 a, uint256 b) internal pure returns (uint256) { return a * b; }
    function div(uint256 a, uint256 b) internal pure returns (uint256) { return a / b; }
}

contract Calc {
    using {Math.add, Math.sub, Math.mul, Math.div} for uint256;

    function compute(uint256 x) external pure returns (uint256) {
        return x;
    }
}
