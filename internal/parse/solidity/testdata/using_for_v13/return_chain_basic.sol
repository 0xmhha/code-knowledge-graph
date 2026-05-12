// W-C W6 V1.3 fixture — chained call dispatch (same-contract function).
// `factory().add(x)` — factory() is a same-contract function returning
// uint256; .add(x) is the using-for dispatch on the returned value.
// Expectations:
//   - 1 EdgeUsesFor: Vault → ChainLib
//   - 1 EdgeCalls (V1.3): Vault.run → ChainLib.add
//     (resolved via inner function `factory`'s return type uint256)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library ChainLib {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract Vault {
    using ChainLib for uint256;

    function factory() internal pure returns (uint256) {
        return 42;
    }

    function run() external pure returns (uint256) {
        return factory().add(1);
    }
}
