// W-C W6 V5 fixture (consumer) — imports math_alpha.sol under
// namespace alias `M` and binds M.mul as the * operator for uint256.
// The resolver should prefer the math_alpha.sol mul over the
// math_beta.sol homonym based on the namespace alias source-path
// hint recorded during runImportAliases.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

import "./math_alpha.sol" as M;

using {M.mul as *} for uint256 global;

contract Calc {
    function compute(uint256 x, uint256 y) external pure returns (uint256) {
        return x * y;
    }
}
