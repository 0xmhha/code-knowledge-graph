// W-C W6 V10 fixture — free function and using-for in the same
// file. Sol 0.8.13+ allows a contract's surrounding free function
// to be bound via the operator-form using directive. The W6 V3
// NodeFunction fallback already resolves this; V10 locks the
// behavior so a future regression to the NodeContract-only path
// fails CI.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

function addOne(uint256 x) pure returns (uint256) {
    return x + 1;
}

using {addOne as *} for uint256 global;

contract Calc {
    function compute(uint256 x) external pure returns (uint256) {
        return x * 2;
    }
}
