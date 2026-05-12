// W-C W6 V1.8 fixture — depth-4 same-contract generic chain.
// `mkA().mkB().mkC().mkD().add(1)` — 4 chain links.
// Expectations:
//   - 1 EdgeUsesFor: Vault → GenLib
//   - 1 EdgeCalls (V1.8 generic walker): Vault.run → GenLib.add
//     (chain: mkA's S1 → S1.mkB's S2 → S2.mkC's S3 → S3.mkD's uint256
//      → GenLib via uint256 binding)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library GenLib {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract S3 {
    function mkD() external pure returns (uint256) {
        return 1;
    }
}

contract S2 {
    function mkC() external pure returns (S3) {
        return S3(address(0));
    }
}

contract S1 {
    function mkB() external pure returns (S2) {
        return S2(address(0));
    }
}

contract Vault {
    using GenLib for uint256;

    function mkA() internal pure returns (S1) {
        return S1(address(0));
    }

    function run() external pure returns (uint256) {
        return mkA().mkB().mkC().mkD().add(1);
    }
}
