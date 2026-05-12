// W-C W6 V1.1 fixture — anonymous parameter (`uint256 /* unused */`)
// must not produce any using_for_call EdgeCalls. The parameter has no
// name field, so it can never be addressed as a receiver.
// Expectations:
//   - 1 EdgeUsesFor: Anon → AnonLib
//   - 0 using_for_call EdgeCalls
//   - Catches a regression where the parameter walker emits a meta
//     PendingRef for nameless parameters (those would never match a
//     real receiver, but the index would still hold a "" → typeName
//     entry that future code might accidentally use).

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library AnonLib {
    function noop(uint256 self) internal pure returns (uint256) {
        return self;
    }
}

contract Anon {
    using AnonLib for uint256;

    // Anonymous parameter — uint256 with no name. Solidity allows this
    // for unused parameters; tree-sitter's `parameter.name` field is
    // optional accordingly.
    function ignore(uint256) external pure returns (uint256) {
        return 0;
    }
}
