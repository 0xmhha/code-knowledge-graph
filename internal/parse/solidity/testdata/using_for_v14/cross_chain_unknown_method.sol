// W-C W6 V1.4 fixture — receiver contract has no matching method.
// `obj.bogus()` doesn't exist on Receiver — V1.4 resolver must drop.
// Expectations:
//   - 1 EdgeUsesFor: Caller → SomeLib (V0 emit unaffected)
//   - 0 V1.4 EdgeCalls (Receiver.bogus not in funcByQName)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SomeLib {
    function ping(uint256 self) internal pure returns (uint256) {
        return self;
    }
}

contract Receiver {
    function legit() external pure returns (uint256) {
        return 1;
    }
}

contract Caller {
    using SomeLib for uint256;

    Receiver public obj;

    function run() external view returns (uint256) {
        // bogus() is not declared on Receiver → inner step drops.
        return obj.bogus().ping();
    }
}
