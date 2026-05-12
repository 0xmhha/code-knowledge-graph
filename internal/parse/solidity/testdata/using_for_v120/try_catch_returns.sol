// W-C W6 V1.20 fixture — try/catch returns clause named param.
// `try foo() returns (uint256 r) { r.add(1); }` — `r` is a function-
// scope variable bound to the success block. Tree-sitter shape:
// try_statement's `parameter` children carry these named return slots
// (NOT in a `return_type` field — different from function_definition).
// V1.19 only walks function_definition.return_type, missing try-catch.
//
// Expectations:
//   - 1 EdgeUsesFor: C → SafeMath
//   - 1 EdgeCalls (V1.20 try-returns named-param): C.f → SafeMath.add

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
            return r.add(1);
        } catch {
            return 0;
        }
    }
}
