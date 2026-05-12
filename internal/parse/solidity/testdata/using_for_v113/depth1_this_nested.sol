// W-C W6 V1.13 fixture — depth-1 this-prefixed nested chain.
// `this.user.balance.add(1)` — this + 1 struct field hop after stateVar.
// V1.9 catches `this.x.method` (depth-0); V1.13 fires at depth-1+.
// Expectations:
//   - 1 EdgeUsesFor: Vault → SafeMath
//   - 1 EdgeCalls (V1.13): Vault.run → SafeMath.add

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

    UserData public user;

    function run() external view returns (uint256) {
        return this.user.balance.add(1);
    }
}
