// W-C W6 V1.13 fixture — depth-3 this-prefixed nested chain.
// `this.org.user.account.balance.add(1)` — this + 3 struct field hops.
// Confirms generic walker handles arbitrary depth (not just 1).
// Expectations:
//   - 1 EdgeUsesFor: Top → DeepLib
//   - 1 EdgeCalls (V1.13): Top.run → DeepLib.add

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

contract Top {
    using DeepLib for uint256;

    Org public org;

    function run() external view returns (uint256) {
        return this.org.user.account.balance.add(1);
    }
}
