// W-C W6 V1.22 fixture — modifier parameter as using-for receiver.
// `modifier hasBalance(uint256 amount) { amount.add(0); _; }` —
// modifier_definition is a separate AST node from function_definition.
// V1.1's emitParameterMetaPending is only called from runFunctionDecl,
// so modifier parameters are not indexed → false-negative on
// `amount.add(0)` dispatch.
//
// Expectations:
//   - 1 EdgeUsesFor: C → SafeMath
//   - 1 EdgeCalls (V1.22 modifier param): hasBalance → SafeMath.add

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract C {
    using SafeMath for uint256;

    modifier hasBalance(uint256 amount) {
        amount.add(0); // V1.22: modifier param amount → uint256 → SafeMath.add
        _;
    }

    function f() external hasBalance(10) returns (uint256) {
        return 1;
    }
}
