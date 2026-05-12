// W-C W6 V1.1 fixture — both state-var and parameter receivers in the
// same contract dispatch correctly. The same library/method is reused
// for both receivers; the resolver must emit two distinct EdgeCalls.
// Expectations:
//   - 1 EdgeUsesFor: Mixed → Helper
//   - 2 EdgeCalls:
//       Mixed.bumpState → Helper.bump   (state-var receiver: counter)
//       Mixed.bumpParam → Helper.bump   (parameter receiver: v)
//   - All EXTRACTED.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library Helper {
    function bump(uint256 self) internal pure returns (uint256) {
        return self + 1;
    }
}

contract Mixed {
    using Helper for uint256;

    uint256 public counter;

    function bumpState() external {
        counter = counter.bump();
    }

    function bumpParam(uint256 v) external pure returns (uint256) {
        return v.bump();
    }
}
