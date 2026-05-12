// W-C W6 V1.9 fixture — `this.<state-var>.<method>(...)` shape.
// `this` is implicit current-contract reference; resolver treats
// `balance` as a state variable on Vault (same as V1.0 bare-name
// `balance.method`).
// Expectations:
//   - 1 EdgeUsesFor: Vault → ThisLib
//   - 1 EdgeCalls: Vault.run → ThisLib.add
//     (via this.balance → balance state-var (uint256) → ThisLib binding)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library ThisLib {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract Vault {
    using ThisLib for uint256;

    uint256 public balance;

    function run() external view returns (uint256) {
        return this.balance.add(1);
    }
}
