// W-C W6 V1.5 fixture — depth-2 chain where the innermost function
// returns a primitive type. The middle step's namespace lookup
// (`uint256.something`) misses cleanly — V1.5 drops without false
// positives even though the predicate matches syntactically.
//
// Expectations:
//   - 1 EdgeUsesFor: Plain → PlainLib
//   - 0 V1.5 EdgeCalls (uint256 not a known container; funcByQName
//     ["uint256.foo"] miss → drop)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library PlainLib {
    function bump(uint256 self) internal pure returns (uint256) {
        return self + 1;
    }
}

contract Plain {
    using PlainLib for uint256;

    function makeNumber() internal pure returns (uint256) {
        return 42;
    }

    function run() external pure returns (uint256) {
        // makeNumber() returns uint256 (primitive). The middle step
        // .foo() would need uint256 to expose foo — it doesn't.
        // V1.5 resolver looks up `uint256.foo` in funcByQName → miss.
        return makeNumber().foo().bump();
    }
}
