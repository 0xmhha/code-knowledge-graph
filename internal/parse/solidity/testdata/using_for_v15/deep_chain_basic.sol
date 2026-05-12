// W-C W6 V1.5 fixture — depth-2 chained dispatch.
// `mkStep1().step2().add(1)` — mkStep1() returns Stage, Stage.step2()
// returns uint256, .add(1) resolves through using ChainLib for uint256.
// Expectations:
//   - 1 EdgeUsesFor: Vault → ChainLib
//   - 1 EdgeCalls (V1.5): Vault.run → ChainLib.add
//     (resolves via mkStep1's Stage return → Stage.step2's uint256
//      return → ChainLib via uint256 binding)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library ChainLib {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract Stage {
    function step2() external pure returns (uint256) {
        return 100;
    }
}

contract Vault {
    using ChainLib for uint256;

    function mkStep1() internal pure returns (Stage) {
        return Stage(address(0));
    }

    function run() external pure returns (uint256) {
        return mkStep1().step2().add(1);
    }
}
