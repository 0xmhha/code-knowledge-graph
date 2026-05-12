// W-C W6 V1.6 fixture — deep cross-contract chain where the middle
// method doesn't exist on the inner function's return type. Resolver
// must drop — no false-positive emission.
//
// Expectations:
//   - 1 EdgeUsesFor: Caller → SomeLib
//   - 0 V1.6 EdgeCalls (Stage.missingMethod not in funcByQName)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SomeLib {
    function ping(uint256 self) internal pure returns (uint256) {
        return self;
    }
}

contract Stage {
    function legitMethod() external pure returns (uint256) {
        return 1;
    }
}

contract Producer {
    function makeStage() external pure returns (Stage) {
        return Stage(address(0));
    }
}

contract Caller {
    using SomeLib for uint256;

    Producer public producer;

    function run() external view returns (uint256) {
        // missingMethod() not declared on Stage → V1.6 step 5 drops.
        return producer.makeStage().missingMethod().ping();
    }
}
