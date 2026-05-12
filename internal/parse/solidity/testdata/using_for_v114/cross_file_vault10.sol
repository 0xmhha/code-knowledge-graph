// W-C W6 V1.14 fixture (cross-file, V1.10 caller side).
// `user.balance.add(1)` — V1.10 depth-1 struct-field receiver, but
// UserData (struct) and SafeMath (library) live in cross_file_lib.sol.
// Tests that structFieldTypes + bindings + funcByQName all resolve
// across file boundary. Expected ConfInferred (caller file ≠ library file).

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Vault10 {
    using SafeMath for uint256;

    UserData public user;

    function run() external view returns (uint256) {
        return user.balance.add(1);
    }
}
