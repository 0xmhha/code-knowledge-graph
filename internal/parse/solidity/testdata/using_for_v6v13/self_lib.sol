// W-C W6 V13 fixture — library binds itself as a using-for
// target. Sol allows a library to declare a using directive that
// references itself; the binding still resolves via the standard
// byName[NodeContract] lookup since libraries are NodeContract +
// SubKind="library".

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SelfLib {
    using SelfLib for uint256;

    function ping(uint256 x) internal pure returns (uint256) {
        return x + 1;
    }
}
