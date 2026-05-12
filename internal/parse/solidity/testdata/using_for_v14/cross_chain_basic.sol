// W-C W6 V1.4 fixture — cross-contract chained dispatch.
// `factory.create()` returns uint256 from another contract; `.bump()`
// then resolves through Vault's using-for binding for uint256.
// Expectations:
//   - 1 EdgeUsesFor: Vault → ChainLib
//   - 1 EdgeCalls (V1.4): Vault.run → ChainLib.bump
//     (chain: factory state-var → Factory contract → create()'s uint256
//      return → ChainLib via uint256 binding → ChainLib.bump)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library ChainLib {
    function bump(uint256 self) internal pure returns (uint256) {
        return self + 1;
    }
}

contract Factory {
    function create() external pure returns (uint256) {
        return 42;
    }
}

contract Vault {
    using ChainLib for uint256;

    Factory public factory;

    function run() external view returns (uint256) {
        return factory.create().bump();
    }
}
