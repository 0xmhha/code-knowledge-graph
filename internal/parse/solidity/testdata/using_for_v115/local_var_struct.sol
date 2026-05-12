// W-C W6 V1.15 fixture — local-var receiver feeding into V1.10 struct-
// field walker. `UserData memory u = ...; u.balance.add(1)` — local
// variable `u` is a struct, V1.10 walker traverses to `balance` field
// then dispatches via SafeMath binding.
// Expectations:
//   - 1 EdgeUsesFor: Vault → SafeMath
//   - 1 EdgeCalls (V1.10 with V1.15 receiver fallback): Vault.handle → SafeMath.add

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

struct UserData {
    uint256 balance;
}

contract Vault {
    using SafeMath for uint256;

    function handle(uint256 seed) external pure returns (uint256) {
        UserData memory u = UserData(seed);
        return u.balance.add(1);
    }
}
