// W-C W10 V8 fixture — self-reference cast vs arbitrary-address
// cast.
//
// `payable(this).call(...)` and `address(this).call(...)` re-enter
// the contract's fallback() / receive() path — the receiver is
// the same contract, not an arbitrary external address. The V8
// marker HasSelfReentrantCall fires INSTEAD of HasExternalCall
// for those shapes. Arbitrary-address cast (`payable(target)`)
// keeps the V5 behaviour with HasExternalCall=true.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract SelfCaster {
    function reentrant(bytes memory data) external returns (bool) {
        // V8: self-reentrant.
        (bool ok, ) = payable(this).call(data);
        return ok;
    }

    function addressSelf(bytes memory data) external returns (bool) {
        // V8: self-reentrant via address(this).
        (bool ok, ) = address(this).call(data);
        return ok;
    }

    function externalRelay(address target, bytes memory data) external returns (bool) {
        // V5 path: arbitrary-address cast. HasExternalCall=true,
        // HasSelfReentrantCall=false.
        (bool ok, ) = payable(target).call(data);
        return ok;
    }

    function noop() external pure {}
}
