// W-C W10 V17 fixture — modifier-scope self-cast. The cast walker
// (W10 V5/V8/V12) resolves the enclosing callable through
// nearestFunctionQnameAndStart, which W6 V1.22 taught to recognise
// modifier_definition the same way it recognises function_definition.
// runDecl(NodeModifier) emits a NodeModifier whose (qname, startByte)
// pair matches what nearestFunctionQnameAndStart returns — so the
// HasSelfReentrantCall marker should land on the NodeModifier row.
//
// The pattern matters because modifiers commonly wrap *other people's*
// functions: a malicious or careless `_; payable(this).call(...)`
// silently re-enters the caller before the function returns control.
// This is a documented exploit shape (e.g. callback-after-state
// modifiers in DAO-style contracts) and the marker must surface it.
//
//   - GuardSelf.reentrantGuard : modifier body contains
//     `payable(this).call("")` after the `_;` placeholder.
//   - GuardTransfer.refundGuard: same shape with `payable(this).transfer(0)`,
//     exercising the V12 value-transfer admission path.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract GuardSelf {
    modifier reentrantGuard() {
        _;
        (bool ok, ) = payable(this).call("");
        require(ok, "guard self-call failed");
    }

    function protected() external reentrantGuard {}
}

contract GuardTransfer {
    modifier refundGuard() {
        _;
        payable(this).transfer(0);
    }

    function drain() external payable refundGuard {}
}
