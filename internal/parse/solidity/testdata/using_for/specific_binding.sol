// W-C W6 fixture — single contract binds a single library to a specific type.
// Expectations:
//   - 1 EdgeUsesFor: Vault → SafeMath, ConfExtracted (same-file).

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract Vault {
    using SafeMath for uint256;

    uint256 public balance;

    function deposit(uint256 amount) external {
        balance = balance + amount;
    }
}
