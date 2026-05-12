// W-C W6 V1.19 fixture — named return parameter as using-for receiver.
// `function f() returns (uint256 result)` declares `result` as a
// function-scope variable initialized to zero. Inside f(), `result`
// can be assigned and read like a local. The pre-V1.19 parser missed
// these — only direct parameter children of function_definition were
// indexed into paramTypes; return_type's parameter children were
// captured for type-only purposes (V1.3 first-return) but never paired
// to their name.
//
// Expectations:
//   - 1 EdgeUsesFor: C → SafeMath
//   - 1 EdgeCalls (V1.19 named-return-param receiver): C.f → SafeMath.add

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract C {
    using SafeMath for uint256;

    function f() external pure returns (uint256 result) {
        result = 5;
        return result.add(1);
    }
}
