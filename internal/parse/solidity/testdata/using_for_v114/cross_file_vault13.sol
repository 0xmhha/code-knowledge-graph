// W-C W6 V1.14 fixture (cross-file, V1.13 this-prefixed depth-2 caller).
// `this.org.user.balance.add(1)` — V1.13 this-prefixed nested chain with
// N=2 struct hops, struct types (Org / UserData) and library (SafeMath)
// in cross_file_lib.sol. Exercises this-prefixed walker + cross-file
// struct chain + cross-file library resolution simultaneously.
// Expected ConfInferred (caller file ≠ library file).

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Vault13 {
    using SafeMath for uint256;

    Org public org;

    function run() external view returns (uint256) {
        return this.org.user.balance.add(1);
    }
}
