// W-C W6 V1.11 fixture — middle field is not a struct (it's a
// primitive). V1.11 expects field1Type to itself be a struct so its
// fields can be walked. Primitive field1Type → step 4 drops.
// Expectations:
//   - 1 EdgeUsesFor: Caller → SomeLib
//   - 0 V1.11 EdgeCalls (balance is uint256, not a struct → step 4
//     structFieldTypes[uint256] miss → drop)

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
        // user.balance is uint256 → can't access .anything → V1.11 drop.
        // (V1.10 would catch user.balance.tag, but V1.11's predicate
        // requires depth-2 nesting which this fixture has syntactically,
        // even though it's semantically illegal Sol.)
        return user.balance.anything.tag();
    }
}
