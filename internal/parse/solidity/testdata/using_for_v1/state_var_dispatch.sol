// W-C W6 V1.0 fixture — state-variable receiver dispatch resolution.
// Expectations:
//   - 1 EdgeUsesFor: Vault → SafeMath  (V0 binding edge, unchanged)
//   - 2 EdgeCalls (V1.0 method-call resolution):
//       Vault.deposit  → SafeMath.add      (balance.add(amount))
//       Vault.withdraw → SafeMath.sub      (balance.sub(amount))
//   - All EXTRACTED (same-file).

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
    function sub(uint256 a, uint256 b) internal pure returns (uint256) {
        return a - b;
    }
}

contract Vault {
    using SafeMath for uint256;

    uint256 public balance;

    function deposit(uint256 amount) external {
        balance = balance.add(amount);
    }

    function withdraw(uint256 amount) external {
        balance = balance.sub(amount);
    }
}
