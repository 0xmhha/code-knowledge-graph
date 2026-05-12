// W-C W6 V1.16 fixture — typed tuple slot but the slot's type has no
// using-for binding (false-positive guard). Only the bound slot
// resolves; the other is silently inert.
// Expectations:
//   - 1 EdgeUsesFor: Caller → SafeMath
//   - 1 EdgeCalls (V1.16 → V1.0): Caller.run → SafeMath.add
//     (only uint256 slot has a binding; the bytes32 slot has none, so
//     no false-positive EdgeCalls is created — even though both slots
//     are V1.16 typed and indexed in localVarTypes.)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 p, uint256 q) internal pure returns (uint256) {
        return p + q;
    }
}

contract Caller {
    using SafeMath for uint256; // binds uint256 only

    function pair() internal pure returns (uint256, bytes32) {
        return (3, bytes32(0));
    }

    function run() external pure returns (uint256) {
        (uint256 n, bytes32 h) = pair();
        h; // silence unused warning
        // n.add(2) resolves via V1.16 tuple slot (uint256 → SafeMath).
        // h has no binding (no `using ... for bytes32`), so even if the
        // source wrote `h.add(2)` it would drop. Asserts no false-positive
        // EdgeCalls from the bytes32 slot.
        return n.add(2);
    }
}
