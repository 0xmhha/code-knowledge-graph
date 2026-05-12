// W-C W6 V1.2 fixture — child contract overrides an inherited binding
// for the same type. Solidity scoping (and our V1.2 BFS) preserves the
// child's local declaration when both reference the same typeName.
// Expectations:
//   - 2 EdgeUsesFor: P → InheritedLib, C → ChildLib
//   - 1 EdgeCalls: C.run → ChildLib.tag  (NOT InheritedLib.tag —
//                                          child binding shadows parent)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library InheritedLib {
    function tag(uint256 self) internal pure returns (uint256) {
        return self;
    }
}

library ChildLib {
    function tag(uint256 self) internal pure returns (uint256) {
        return self + 99;
    }
}

contract P {
    using InheritedLib for uint256;
}

contract C is P {
    using ChildLib for uint256;

    uint256 public v;

    function run() external {
        v = v.tag();
    }
}
