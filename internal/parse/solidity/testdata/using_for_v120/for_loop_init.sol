// W-C W6 V1.20 fixture — for-loop init variable as using-for receiver.
// `for (uint256 i = 0; i < 10; ...) { i.add(1); }` — the init clause's
// `uint256 i` is a variable_declaration_statement nested inside a
// for_statement. V1.15's emitLocalVarMetaPending uses recursive descent
// through the function body, so i should be captured.
//
// Expectations:
//   - 1 EdgeUsesFor: C → SafeMath
//   - 1 EdgeCalls (V1.15 via recursive descent into for_statement):
//     C.f → SafeMath.add

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract C {
    using SafeMath for uint256;

    function f() external pure returns (uint256 acc) {
        for (uint256 i = 0; i < 3; i = i + 1) {
            acc = i.add(acc);
        }
        return acc;
    }
}
