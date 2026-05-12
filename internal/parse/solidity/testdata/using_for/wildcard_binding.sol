// W-C W6 fixture — wildcard binding (`using Lib for *`).
// Expectations:
//   - 1 EdgeUsesFor: Universal → Helpers, ConfExtracted.
//   - V0 does not surface the `*` type marker on the edge — the edge
//     itself is identical in shape to specific_binding. (§4.6.6 V0 limit.)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library Helpers {
    function ping() internal pure returns (uint256) {
        return 1;
    }
}

contract Universal {
    using Helpers for *;

    function trigger() external pure returns (uint256) {
        return 42;
    }
}
