// W-C W6 V1.22 fixture — modifier body local variable as using-for
// receiver. `modifier checkAmount() { uint256 base = 10; base.add(1); _; }` —
// modifier_definition's function_body containing variable_declaration_
// statement. V1.15's emitLocalVarMetaPending was only called from
// runFunctionDecl, so modifier locals were not indexed.
//
// Expectations:
//   - 1 EdgeUsesFor: C → SafeMath
//   - 1 EdgeCalls (V1.22 modifier local): checkAmount → SafeMath.add

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract C {
    using SafeMath for uint256;

    modifier checkAmount() {
        uint256 base = 10;
        base.add(1);
        _;
    }

    function f() external checkAmount returns (uint256) {
        return 1;
    }
}
