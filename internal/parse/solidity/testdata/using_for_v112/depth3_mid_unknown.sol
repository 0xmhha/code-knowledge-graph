// W-C W6 V1.12 fixture — depth-3 chain but middle field doesn't exist
// on its parent struct. Resolver must drop.
// Expectations:
//   - 1 EdgeUsesFor: Caller → SomeLib
//   - 0 V1.12 EdgeCalls (UserData.missingMid → step 4 drop in iter)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SomeLib {
    function tag(uint256 self) internal pure returns (uint256) {
        return self;
    }
}

struct Account {
    uint256 balance;
}

struct UserData {
    Account account;
}

struct Org {
    UserData user;
}

contract Caller {
    using SomeLib for uint256;

    Org public org;

    function run() external view returns (uint256) {
        // missingMid not declared on UserData → V1.12 walker drops at
        // the middle hop.
        return org.user.missingMid.balance.tag();
    }
}
