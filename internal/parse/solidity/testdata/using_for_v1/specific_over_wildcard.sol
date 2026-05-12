// W-C W6 V1.0 fixture — Q9-3 (a) specific-first wildcard fallback.
// Both directives are present in the same contract; the specific
// binding (uint256) should win over the wildcard for state vars typed
// uint256.
// Expectations:
//   - 2 EdgeUsesFor: Specifics → SpecificLib, Specifics → FallbackLib
//   - 1 EdgeCalls:  Specifics.touch → SpecificLib.specificOp
//                   (NOT FallbackLib.fallbackOp — specific wins)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SpecificLib {
    function specificOp(uint256 x) internal pure returns (uint256) {
        return x + 100;
    }
}

library FallbackLib {
    function fallbackOp(uint256 x) internal pure returns (uint256) {
        return x;
    }
}

contract Specifics {
    using SpecificLib for uint256;
    using FallbackLib for *;

    uint256 public value;

    function touch() external {
        value = value.specificOp();
    }
}
