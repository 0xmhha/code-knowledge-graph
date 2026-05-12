// W-C W6 V1.18 fixture (cross-file, V1.16 tuple caller).
// `(UserData memory u, uint256 n) = pair(); u.balance.add(n)` —
// tuple destructuring with both slots typed. struct (UserData) and
// library (SafeMath) live in cross_file_lib.sol. Walker must resolve
// across the file boundary at ConfInferred.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Vault {
    using SafeMath for uint256;

    function pair() internal pure returns (UserData memory, uint256) {
        return (UserData(100), 7);
    }

    function run() external pure returns (uint256) {
        (UserData memory u, uint256 n) = pair();
        return u.balance.add(n);
    }
}
