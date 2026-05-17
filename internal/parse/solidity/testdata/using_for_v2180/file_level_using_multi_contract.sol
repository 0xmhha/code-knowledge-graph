// W-C W6 V2.18 fixture — file-level using directive applied to
// MULTIPLE contracts in the same file. Validates that the recovered
// binding fans out to every container (Sol semantics: file-level
// using applies to all contracts/libraries/interfaces in the file).
//
// Expectations (post-V2.18):
//   - 2 EdgeUsesFor: VaultA → SafeMath, VaultB → SafeMath
//   - 2 EdgeCalls:   VaultA.compute → SafeMath.add,
//                    VaultB.compute → SafeMath.add

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.13;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

using SafeMath for uint256;

contract VaultA {
    uint256 public x;
    function compute() external view returns (uint256) { return x.add(1); }
}

contract VaultB {
    uint256 public y;
    function compute() external view returns (uint256) { return y.add(2); }
}
