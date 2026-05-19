// W-C W6 V7 fixture (consumer) — uses a 3-segment nested path
// `M.SubMath.addOne` in a file-level operator-form using
// directive. M is a namespace alias pointing at lib.sol so the
// resolver must prefer lib.sol's SubMath over the lib_alt.sol
// homonym.
//
// Current V7 contract: the path hint correctly routes through M's
// recorded source path; the trailing method name (addOne) is not
// yet propagated to the binding edge (the EdgeUsesFor dst is the
// SubMath library, not addOne specifically). Method-name
// propagation is W6 V8+ scope.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

import "./lib.sol" as M;

using {M.SubMath.addOne as +} for uint256 global;

contract Calc {
    function compute(uint256 x, uint256 y) external pure returns (uint256) {
        return x + y;
    }
}
