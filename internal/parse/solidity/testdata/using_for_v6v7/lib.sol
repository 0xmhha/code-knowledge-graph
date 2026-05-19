// W-C W6 V7 fixture — provides a library SubMath whose member
// addOne is bound via a namespace-aliased nested path in the
// consumer. There's a homonym SubMath in lib_alt.sol; the
// resolver must prefer this one because the namespace alias `M`
// points at this file.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

library SubMath {
    function addOne(uint256 x) internal pure returns (uint256) {
        return x + 1;
    }
}
