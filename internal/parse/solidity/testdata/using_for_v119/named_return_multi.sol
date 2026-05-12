// W-C W6 V1.19 fixture — multiple named return parameters; each must
// be indexed independently. Receiver `a` and `b` both dispatch to
// SafeMath.add via paramTypes lookup.
// Expectations:
//   - 1 EdgeUsesFor: C → SafeMath
//   - 2 EdgeCalls: C.f → SafeMath.add (line for a.add) AND
//                  C.f → SafeMath.add (line for b.add)
//     (aggregated as 2 separate edges in the graph — same src/dst but
//      distinct Line; collectUsingForCalls returns one entry per
//      EdgeCalls regardless of Count.)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 x, uint256 y) internal pure returns (uint256) {
        return x + y;
    }
}

contract C {
    using SafeMath for uint256;

    function f() external pure returns (uint256 a, uint256 b) {
        a = 1;
        b = 2;
        a.add(b);  // uses both named return params
        return (a, b);
    }
}
