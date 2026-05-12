// W-C W6 V1.13 fixture — depth-1 this-prefixed chain where the middle
// field doesn't exist on the stateVar's struct. Resolver must drop.
// Expectations:
//   - 1 EdgeUsesFor: Caller → SomeLib
//   - 0 V1.13 EdgeCalls (missingField not declared on UserData → step 4
//     drop in walker)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SomeLib {
    function tag(uint256 self) internal pure returns (uint256) {
        return self;
    }
}

struct UserData {
    uint256 balance;
}

contract Caller {
    using SomeLib for uint256;

    UserData public user;

    function run() external view returns (uint256) {
        // missingField not declared on UserData → V1.13 walker drops.
        return this.user.missingField.tag();
    }
}
