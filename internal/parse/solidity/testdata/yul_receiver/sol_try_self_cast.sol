// W-C W10 V16 fixture — try-statement self-cast. The cast walker
// queries every member_expression in the whole tree and resolves the
// enclosing callable via nearestFunctionQnameAndStart. try_statement
// is *not* a callable shape — the walker should traverse past it and
// attach the reentrancy marker to the outer function_definition that
// physically contains the try block.
//
// Without an explicit lock here, a future refactor that introduces a
// query scope or stops the walk at try_statement would silently lose
// the marker on every try-wrapped self-call — a textbook blind spot
// because developers often *believe* try gives them re-entrancy
// safety (it does not: the callee can still re-enter the caller
// before the try completes).
//
//   - TryCall.attack    : enclosing fn contains a try block whose
//                         expression is `payable(this).call(...)` —
//                         the self-call surface itself.
//   - TryTransfer.drain : same shape with `.transfer(0)`, exercising
//                         the V12 value-transfer admission path.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract TryCall {
    function attack() external payable {
        try this.recv() {} catch {}
        // The self-call surface — payable(this).call(...) — sits
        // inside the same function body but next to the try block.
        // The walker must reach attack() as the enclosing callable.
        (bool ok, ) = payable(this).call("");
        require(ok, "self-call failed");
    }

    function recv() external payable {}
}

contract TryTransfer {
    function drain() external payable {
        try this.sink() {} catch {}
        payable(this).transfer(0);
    }

    function sink() external payable {}
}
