// W-C W6 V1.16 fixture — tuple slot is a struct, feeds into V1.10
// struct-field walker. `(UserData memory u, uint256 n) = unpack();
// u.balance.add(n)` — first tuple slot is struct, V1.10 walker resolves
// across to SafeMath.
// Expectations:
//   - 1 EdgeUsesFor: Handler → SafeMath
//   - 1 EdgeCalls (V1.16 → V1.10): Handler.process → SafeMath.add

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 x, uint256 y) internal pure returns (uint256) {
        return x + y;
    }
}

struct UserData {
    uint256 balance;
}

contract Handler {
    using SafeMath for uint256;

    function unpack() internal pure returns (UserData memory, uint256) {
        return (UserData(100), 7);
    }

    function process() external pure returns (uint256) {
        (UserData memory u, uint256 n) = unpack();
        return u.balance.add(n);
    }
}
