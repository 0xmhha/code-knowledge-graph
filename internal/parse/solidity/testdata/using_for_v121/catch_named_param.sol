// W-C W6 V1.21 fixture — catch_clause named parameter as using-for
// receiver. `catch Panic(uint256 errorCode) { errorCode.add(1) }` —
// `errorCode` is a parameter scoped to the catch body. Tree-sitter
// exposes it as a `parameter` direct child of catch_clause (alongside
// the catch error-type identifier). V1.20 added try_statement scope
// but did not descend into catch_clause's own parameter slot, so
// `errorCode.method()` dispatch drops pre-V1.21.
//
// Expectations:
//   - 1 EdgeUsesFor: C → SafeMath
//   - 1 EdgeCalls (V1.21 catch-param): C.f → SafeMath.add

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
            return r;
        } catch Panic(uint256 errorCode) {
            return errorCode.add(1);
        }
    }
}
