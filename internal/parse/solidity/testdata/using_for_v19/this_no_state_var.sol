// W-C W6 V1.9 fixture — `this.<not-a-state-var>.<method>()`. The inner
// property doesn't match any declared state variable on the current
// contract. Resolver must drop (stateVarTypes lookup misses).
//
// Expectations:
//   - 1 EdgeUsesFor: NoVar → SomeLib
//   - 0 V1.9 EdgeCalls (this.missingField → stateVarTypes miss)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SomeLib {
    function tag(uint256 self) internal pure returns (uint256) {
        return self;
    }
}

contract NoVar {
    using SomeLib for uint256;

    function run() external view returns (uint256) {
        // missingField isn't declared on NoVar → V1.9 resolver looks
        // up "missingField" in stateVarTypes[NoVar] → miss → drop.
        return this.missingField.tag();
    }
}
