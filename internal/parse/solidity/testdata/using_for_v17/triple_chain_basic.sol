// W-C W6 V1.7 fixture — depth-3 same-contract chain.
// `mkA().mkB().mkC().add(1)` — three local functions chain through
// Stage1 → Stage2 → uint256, then .add(1) resolves through using
// ChainLib for uint256.
// Expectations:
//   - 1 EdgeUsesFor: Vault → ChainLib
//   - 1 EdgeCalls (V1.7): Vault.run → ChainLib.add
//     (chain: mkA()'s Stage1 return → Stage1.mkB()'s Stage2 return →
//      Stage2.mkC()'s uint256 return → ChainLib via uint256 binding)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library ChainLib {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract Stage2 {
    function mkC() external pure returns (uint256) {
        return 100;
    }
}

contract Stage1 {
    function mkB() external pure returns (Stage2) {
        return Stage2(address(0));
    }
}

contract Vault {
    using ChainLib for uint256;

    function mkA() internal pure returns (Stage1) {
        return Stage1(address(0));
    }

    function run() external pure returns (uint256) {
        return mkA().mkB().mkC().add(1);
    }
}
