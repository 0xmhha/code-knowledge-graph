// W-C W6 V1.7 fixture — middle step in the depth-3 chain doesn't
// exist on the inner function's return type. Resolver must drop.
// Expectations:
//   - 1 EdgeUsesFor: Caller → ChainLib
//   - 0 V1.7 EdgeCalls (Stage1.missingMid not in funcByQName)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library ChainLib {
    function tag(uint256 self) internal pure returns (uint256) {
        return self;
    }
}

contract Stage2 {
    function mkC() external pure returns (uint256) {
        return 1;
    }
}

contract Stage1 {
    function legitMid() external pure returns (Stage2) {
        return Stage2(address(0));
    }
}

contract Caller {
    using ChainLib for uint256;

    function mkA() internal pure returns (Stage1) {
        return Stage1(address(0));
    }

    function run() external pure returns (uint256) {
        // missingMid() not declared on Stage1 → V1.7 step 4 drops.
        return mkA().missingMid().mkC().tag();
    }
}
