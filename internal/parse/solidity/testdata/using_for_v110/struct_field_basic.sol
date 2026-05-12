// W-C W6 V1.10 fixture — struct-field receiver dispatch.
// `info.amount.add(1)` — info is a UserInfo struct state-var; amount
// is a uint256 field of UserInfo; .add(1) resolves through using
// StructLib for uint256.
// Expectations:
//   - 1 EdgeUsesFor: Vault → StructLib
//   - 1 EdgeCalls: Vault.run → StructLib.add
//     (chain: info state-var → UserInfo type → UserInfo.amount field
//      → uint256 → StructLib via uint256 binding)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library StructLib {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

struct UserInfo {
    uint256 amount;
    address owner;
}

contract Vault {
    using StructLib for uint256;

    UserInfo public info;

    function run() external view returns (uint256) {
        return info.amount.add(1);
    }
}
