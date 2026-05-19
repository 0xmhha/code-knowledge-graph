// W-C W6 V4 fixture (consumer half) — imports math.sol under the
// namespace alias `M` and binds its `mul` free function as the
// `*` operator for uint256 via the operator-form using directive.
//
// Pre-V4 behaviour: the file-level operator-form walker extracted
// `M` as the leading identifier, hit v.namespaceAliases[M]==true,
// and skipped the entry — net result 0 EdgeUsesFor.
//
// V4 behaviour: the walker recognises the namespace-aliased shape
// and uses the dotted tail `mul` as the binding target. The
// resolver's NodeFunction fallback (W6 V3) then matches the
// imported free function across files.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

import "./math.sol" as M;

using {M.mul as *} for uint256 global;

contract Calc {
    function compute(uint256 x, uint256 y) external pure returns (uint256) {
        return x * y;
    }
}
