// W-C W6 V1.9 fixture — both `this.x.method()` and bare `x.method()`
// in the same function. V1.0 catches the bare form; V1.9 catches the
// `this.` prefix form. Both must resolve to the same EdgeCalls (same
// caller, same library method).
//
// Expectations:
//   - 1 EdgeUsesFor: Sample → SampleLib
//   - 2 EdgeCalls: Sample.run → SampleLib.touch  (twice — both call
//     sites produce one edge each)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SampleLib {
    function touch(uint256 self) internal pure returns (uint256) {
        return self;
    }
}

contract Sample {
    using SampleLib for uint256;

    uint256 public value;

    function run() external view returns (uint256) {
        uint256 a = value.touch();        // V1.0 bare-name
        uint256 b = this.value.touch();   // V1.9 this-prefix
        return a + b;
    }
}
