// W-C W6 fixture — single contract binds 2 libraries to 2 different types.
// Expectations:
//   - 2 EdgeUsesFor: Combined → SafeMathTwo, Combined → AddressTwo
//   - Both ConfExtracted (same-file).
//   - V0: one EdgeUsesFor per directive — no dedup at the (Contract,
//     Library) level. Two distinct libraries → two distinct edges.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMathTwo {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

library AddressTwo {
    function isContract(address account) internal view returns (bool) {
        uint256 size;
        assembly { size := extcodesize(account) }
        return size > 0;
    }
}

contract Combined {
    using SafeMathTwo for uint256;
    using AddressTwo for address;

    function check(uint256 a, address target) external view returns (bool) {
        return AddressTwo.isContract(target);
    }
}
