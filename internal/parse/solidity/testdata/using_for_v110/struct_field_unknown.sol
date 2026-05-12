// W-C W6 V1.10 fixture — receiver is a struct state-var but the
// referenced field doesn't exist on the struct. Resolver must drop.
// Expectations:
//   - 1 EdgeUsesFor: Caller → MaybeLib
//   - 0 V1.10 EdgeCalls (UserData.missingField → structFieldTypes miss)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library MaybeLib {
    function tag(uint256 self) internal pure returns (uint256) {
        return self;
    }
}

struct UserData {
    uint256 balance;
}

contract Caller {
    using MaybeLib for uint256;

    UserData public data;

    function run() external view returns (uint256) {
        // missingField is not declared on UserData → V1.10 resolver
        // structFieldTypes[UserData][missingField] miss → drop.
        return data.missingField.tag();
    }
}
