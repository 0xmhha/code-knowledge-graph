// W-C W6 V1.11 fixture — inner field exists on outer struct, but
// outer field's struct doesn't have inner field. Resolver must drop.
// Expectations:
//   - 1 EdgeUsesFor: Caller → SomeLib
//   - 0 V1.11 EdgeCalls (Account.missingInner → structFieldTypes miss)

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

contract Caller {
    using SomeLib for uint256;

    UserData public user;

    function run() external view returns (uint256) {
        // missingInner isn't on Account → step 4 drops.
        return user.account.missingInner.tag();
    }
}
