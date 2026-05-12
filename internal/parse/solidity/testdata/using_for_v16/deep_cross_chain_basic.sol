// W-C W6 V1.6 fixture — deep cross-contract chained dispatch.
// `factory.makeStage().computeNumber().add(1)` — factory is a Vault
// state var (Factory contract); makeStage() returns Stage; Stage's
// computeNumber() returns uint256; .add(1) resolves through
// `using ChainLib for uint256` binding.
// Expectations:
//   - 1 EdgeUsesFor: Vault → ChainLib
//   - 1 EdgeCalls (V1.6): Vault.run → ChainLib.add
//     (chain: factory state-var → Factory contract → makeStage()'s
//      Stage return → Stage.computeNumber()'s uint256 return →
//      ChainLib via uint256 binding → ChainLib.add)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library ChainLib {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract Stage {
    function computeNumber() external pure returns (uint256) {
        return 100;
    }
}

contract Factory {
    function makeStage() external pure returns (Stage) {
        return Stage(address(0));
    }
}

contract Vault {
    using ChainLib for uint256;

    Factory public factory;

    function run() external view returns (uint256) {
        return factory.makeStage().computeNumber().add(1);
    }
}
