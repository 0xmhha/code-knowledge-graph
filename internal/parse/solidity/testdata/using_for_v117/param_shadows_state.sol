// W-C W6 V1.17 fixture — function parameter shadows state variable.
// param x : uint256 (bound to SafeMath). state-var x : UserData struct
// (no binding). Inside f(uint256 x), receiver `x.add(1)` must resolve
// to the parameter, not the state-var.
//
// Expectations:
//   - 1 EdgeUsesFor: C → SafeMath
//   - 1 EdgeCalls (V1.1 + V1.17 precedence): C.f → SafeMath.add

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

struct UserData {
    address owner;
}

contract C {
    using SafeMath for uint256;

    UserData public x; // state-var x is a struct, no SafeMath binding

    function f(uint256 x) external pure returns (uint256) {
        // param x shadows state-var x. Must resolve to uint256.
        return x.add(1);
    }
}
