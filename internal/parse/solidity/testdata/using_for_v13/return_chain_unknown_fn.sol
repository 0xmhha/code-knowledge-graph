// W-C W6 V1.3 fixture — chained call on an unknown function name.
// The inner identifier doesn't resolve to any local function. V1.3
// must drop without false-positive emission.
// Expectations:
//   - 1 EdgeUsesFor: Caller → SomeLib (V0 emit unaffected)
//   - 0 V1.3 EdgeCalls (mystery() not a known function in this build)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SomeLib {
    function bump(uint256 self) internal pure returns (uint256) {
        return self + 1;
    }
}

contract Caller {
    using SomeLib for uint256;

    function run() external pure returns (uint256) {
        // mystery() is undeclared in this fixture — solc would error,
        // but tree-sitter parses it as a call_expression on an
        // identifier. V1.3 resolver must drop (no funcByQName match).
        return mystery().bump();
    }
}
