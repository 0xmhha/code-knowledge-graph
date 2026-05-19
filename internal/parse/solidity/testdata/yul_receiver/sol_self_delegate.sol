// W-C W10 V9 fixture — self-delegatecall vs self-call.
//
// `address(this).delegatecall(...)` re-executes the contract's
// own code against its own storage — a no-op that's almost
// always a bug. V9 marks HasSelfDelegatecallDead=true alongside
// the V8 HasSelfReentrantCall=true. Other self-call variants
// (call / staticcall) only set HasSelfReentrantCall.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract SelfDelegate {
    function deadDelegate(bytes memory data) external returns (bool) {
        // V9: self-delegatecall. Marks both HasSelfReentrantCall
        // and HasSelfDelegatecallDead.
        (bool ok, ) = address(this).delegatecall(data);
        return ok;
    }

    function reentrantStatic(bytes memory data) external returns (bool) {
        // V8: self-staticcall. HasSelfReentrantCall=true,
        // HasSelfDelegatecallDead=false.
        (bool ok, ) = address(this).staticcall(data);
        return ok;
    }
}
