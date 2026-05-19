// W-C W10 V11 fixture — Yul return / revert with address() as
// memory pointer. The Yul `return(p, s)` builtin treats its
// arguments as (memory pointer, length); passing `address()`
// (the contract's account address) as the pointer is almost
// always a bug — the address is meaningless as a memory offset.
//
// V11 honest scope: audit only. The current pipeline doesn't
// mark this case (no false-positive risk worth the marker
// noise); the test locks the absence of false positives on
// HasSelfDelegatecallDead and HasLowLevelCall for the bogus
// pattern.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract YulReturnAudit {
    // The body intentionally compiles but is semantically broken
    // — the Yul `return` reads memory at the contract address
    // which is not a valid memory offset. V11 audits that our
    // walkers don't accidentally treat this as a low-level call
    // surface.
    function bogusReturn() external view {
        assembly {
            return(address(), 32)
        }
    }
}
