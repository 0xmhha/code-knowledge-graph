// W-C W10 V5 fixture — cast / wrapper shapes for low-level calls.
//
// V4 marked HasExternalCall via the Pass 2 receiver-type lookup for
// bare-identifier receivers. V5 catches Sol's cast wrappers
// directly at Pass 1 since the cast itself is sufficient evidence
// of arbitrary-address dispatch.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Caster {
    uint256 stored;

    // address(t).call(data) - V5 cast shape
    function viaAddressCast(uint256 t, bytes memory data) external returns (bool) {
        (bool ok, ) = address(uint160(t)).call(data);
        return ok;
    }

    // payable(t).call(data) - V5 cast shape
    function viaPayableCast(address t, bytes memory data) external returns (bool) {
        (bool ok, ) = payable(t).call(data);
        return ok;
    }

    // Plain state-var write - no low-level call, HasExternalCall stays false.
    function safeStore(uint256 v) external {
        stored = v;
    }
}
