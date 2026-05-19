// W-C W6 V6 fixture (consumer) — imports origMul from
// math_alpha.sol under the named-import alias `mul`, then binds
// `mul as *` for uint256. The resolver should prefer the
// math_alpha.sol origMul over the math_beta.sol homonym based on
// the named-import alias source-path hint recorded by V6.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

import {origMul as mul} from "./math_alpha.sol";

using {mul as *} for uint256 global;

contract Calc {
    function compute(uint256 x, uint256 y) external pure returns (uint256) {
        return x * y;
    }
}
