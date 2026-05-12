// W-C W6 V1.5 fixture — depth-2 chain where the middle method doesn't
// exist on the inner function's return type. Resolver must drop —
// no false-positive emission for unresolved chain links.
//
// Expectations:
//   - 1 EdgeUsesFor: Caller → SomeLib
//   - 0 V1.5 EdgeCalls (Stage.missingStep not in funcByQName)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SomeLib {
    function ping(uint256 self) internal pure returns (uint256) {
        return self;
    }
}

contract Stage {
    function legitStep() external pure returns (uint256) {
        return 1;
    }
}

contract Caller {
    using SomeLib for uint256;

    function mkStage() internal pure returns (Stage) {
        return Stage(address(0));
    }

    function run() external pure returns (uint256) {
        // missingStep() is undeclared on Stage → middle-step lookup
        // misses → V1.5 drop.
        return mkStage().missingStep().ping();
    }
}
