// W-C W6 V1.11 fixture — depth-2 nested struct field receiver.
// `user.account.balance.add(1)` — user is a UserData struct (state-
// var); account is an Account struct field of UserData; balance is a
// uint256 field of Account. .add(1) resolves through using NestedLib
// for uint256.
// Expectations:
//   - 1 EdgeUsesFor: Vault → NestedLib
//   - 1 EdgeCalls: Vault.run → NestedLib.add
//     (chain: user state-var → UserData struct → account field
//      (Account) → balance field (uint256) → NestedLib via uint256)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library NestedLib {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

struct Account {
    uint256 balance;
    bool frozen;
}

struct UserData {
    Account account;
    address owner;
}

contract Vault {
    using NestedLib for uint256;

    UserData public user;

    function run() external view returns (uint256) {
        return user.account.balance.add(1);
    }
}
