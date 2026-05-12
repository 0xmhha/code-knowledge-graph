// W-C W6 V1.17 fixture — local variable shadows state variable with
// the same name. Per Solidity scoping rules, function-scope (locals,
// parameters) shadows contract-scope (state variables); inner scope
// wins.
//
// State var x is a struct (UserData, not bound to any library).
// Local x in f() is a uint256 (bound to SafeMath).
// The receiver `x.add(1)` inside f() must resolve to the LOCAL x →
// uint256 → SafeMath.add. The pre-V1.17 lookupReceiverType chain
// (state-var first) would resolve to UserData (no binding) and DROP
// the edge — a false negative.
//
// Expectations:
//   - 1 EdgeUsesFor: C → SafeMath
//   - 1 EdgeCalls (V1.0 + V1.17 corrected precedence): C.f → SafeMath.add

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
    using SafeMath for uint256; // bound to uint256 only

    UserData public x; // state-var x is a struct, no SafeMath binding

    function f() external pure returns (uint256) {
        uint256 x = 5; // local x shadows state-var x within this function
        return x.add(1); // must resolve via local (uint256 → SafeMath)
    }
}
