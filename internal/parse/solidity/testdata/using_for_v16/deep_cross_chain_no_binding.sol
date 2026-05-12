// W-C W6 V1.6 fixture — chain resolves fully through the inner method
// chain but the final return type has no binding declared. Resolver
// must drop at the binding lookup step.
//
// Expectations:
//   - 1 EdgeUsesFor: NoBindCaller → MismatchLib (uint256-only binding)
//   - 0 V1.6 EdgeCalls (computeAddress returns address; only uint256
//     is bound — `using MismatchLib for uint256;` doesn't cover address)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library MismatchLib {
    function tag(uint256 self) internal pure returns (uint256) {
        return self;
    }
}

contract Stage {
    function computeAddress() external view returns (address) {
        return address(this);
    }
}

contract Producer {
    function makeStage() external pure returns (Stage) {
        return Stage(address(0));
    }
}

contract NoBindCaller {
    using MismatchLib for uint256;

    Producer public producer;

    function run() external view returns (uint256) {
        // computeAddress returns address; MismatchLib is bound for
        // uint256, not address → final binding lookup misses → drop.
        return producer.makeStage().computeAddress().tag();
    }
}
