// W-C W6 V1.12 fixture — depth-3 generic member chain.
// `org.user.account.balance.add(1)` — 3 struct field hops:
// org(Org struct) → user(UserData) → account(Account) → balance(uint256).
// Expectations:
//   - 1 EdgeUsesFor: Vault → DeepLib
//   - 1 EdgeCalls (V1.12): Vault.run → DeepLib.add
//     (V1.10/V1.11 reject; V1.12 generic walker fires at depth-3.)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library DeepLib {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

struct Account {
    uint256 balance;
}

struct UserData {
    Account account;
}

struct Org {
    UserData user;
}

contract Vault {
    using DeepLib for uint256;

    Org public org;

    function run() external view returns (uint256) {
        return org.user.account.balance.add(1);
    }
}
