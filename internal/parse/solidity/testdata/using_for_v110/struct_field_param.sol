// W-C W6 V1.10 fixture — struct-field receiver where the struct is a
// function parameter (not a state-var). V1.10 resolver falls back to
// paramTypes after stateVarTypes miss.
// Expectations:
//   - 1 EdgeUsesFor: ParamUser → ParamLib
//   - 1 EdgeCalls: ParamUser.process → ParamLib.bump
//     (chain: info param → Record type → Record.value field → uint256
//      → ParamLib)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library ParamLib {
    function bump(uint256 self) internal pure returns (uint256) {
        return self + 1;
    }
}

struct Record {
    uint256 value;
    bool flag;
}

contract ParamUser {
    using ParamLib for uint256;

    function process(Record memory info) external pure returns (uint256) {
        return info.value.bump();
    }
}
