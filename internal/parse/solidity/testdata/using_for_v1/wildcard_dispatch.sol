// W-C W6 V1.0 fixture — wildcard binding (`using Lib for *`) dispatch.
// Expectations:
//   - 1 EdgeUsesFor: Universal → AnyLib
//   - 1 EdgeCalls: Universal.run → AnyLib.boop (counter.boop())
//   - Q9-3 (a) specific-first means a real `for *` is consumed only when
//     no specific binding exists. Here only `*` is declared so the
//     wildcard path fires.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library AnyLib {
    function boop(uint256 self) internal pure returns (uint256) {
        return self + 1;
    }
}

contract Universal {
    using AnyLib for *;

    uint256 public counter;

    function run() external {
        counter = counter.boop();
    }
}
