// W-C W6 fixture — two contracts each bind the same library independently.
// Expectations:
//   - 2 EdgeUsesFor:
//       VaultA → SharedLib    (ConfExtracted, same-file)
//       VaultB → SharedLib    (ConfExtracted, same-file)
//   - The two edges share the same Dst (SharedLib) but distinct Src.
//   - Demonstrates contract-scoped binding semantics: a library can be
//     bound by multiple contracts independently; the graph records each
//     binding as its own edge.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SharedLib {
    function helper(uint256 x) internal pure returns (uint256) {
        return x * 2;
    }
}

contract VaultA {
    using SharedLib for uint256;

    function aOp(uint256 v) external pure returns (uint256) {
        return v;
    }
}

contract VaultB {
    using SharedLib for uint256;

    function bOp(uint256 v) external pure returns (uint256) {
        return v;
    }
}
