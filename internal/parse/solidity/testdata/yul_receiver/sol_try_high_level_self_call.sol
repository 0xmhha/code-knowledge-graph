// W-C W10 V20 fixture — try-wrapped high-level self-call.
//
// This is a cross-axis lockdown: V16 established that
// nearestFunctionQnameAndStart walks transparently through
// try_statement (parent chain reaches the outer function);
// V19 added the HasHighLevelSelfCall marker for typed self-calls.
// V20 asserts the two compose without any extra wiring — a
// `try this.foo() {} catch {}` (or `try IFoo(this).bar()...`)
// should still land HasHighLevelSelfCall=true on the surrounding
// function.
//
// The pattern matters because try-wrapping a self-call is the
// canonical "soft fail on re-entry" idiom developers reach for —
// and is exactly when the re-entrancy surface stays open (the
// callee re-enters before the try-block completes).
//
//   - TryHighSelf.spawn          : try this.target() {} catch {}
//   - TryHighInterface.trigger   : try IFoo(address(this)).foo() {}
//                                  catch {}

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

interface IFoo {
    function foo() external;
}

contract TryHighSelf {
    function spawn() external {
        try this.target() {} catch {}
    }

    function target() external {}
}

contract TryHighInterface is IFoo {
    function trigger() external {
        try IFoo(address(this)).foo() {} catch {}
    }

    function foo() external override {}
}
