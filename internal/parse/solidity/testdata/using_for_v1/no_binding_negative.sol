// W-C W6 V1.0 fixture — negative case. Method calls on identifiers but
// no using directive in scope. V1.0 must emit ZERO EdgeCalls via the
// using_for_call branch (it may emit unresolved-method drops as before;
// the assertion is "no SafeMath.* EdgeCalls").
//
// Defensive purpose: catches a regression where the receiver-type
// resolver fires without a corresponding binding entry.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library Unbound {
    function unused(uint256 x) internal pure returns (uint256) {
        return x;
    }
}

contract NoBinding {
    uint256 public value;

    function update(uint256 amount) external {
        // No `using Unbound for uint256;` here.
        // value.unused(amount) would be unresolved — V0 drop expected.
        value = amount;
    }
}
