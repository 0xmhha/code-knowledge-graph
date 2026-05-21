// W-C W10 V23 fixture probe — Yul self-call via address().
//
// In assembly blocks, `address()` returns the current contract's
// address — a self-receiver by construction. The standard low-
// level call shape `call(gas, addr, val, in, insz, out, outsz)`
// dispatches a message to addr; when addr is `address()` the
// dispatch re-enters the same contract.
//
// This probe asks: does any current marker (HasSelfReentrantCall
// from the Sol cast walker, YulBuiltins from assembly_marker,
// or HasExternalCall) flag the self-receiver pattern, or does it
// fall through unmarked?
//
//   - AsmSelf.fire        : call(gas(), address(), 0, 0, 0, 0, 0)
//   - AsmSelfDelegate.fire: delegatecall(gas(), address(), 0, 0, 0, 0)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract AsmSelf {
    function fire() external {
        assembly {
            let ok := call(gas(), address(), 0, 0, 0, 0, 0)
        }
    }
}

contract AsmSelfDelegate {
    function fire() external {
        assembly {
            let ok := delegatecall(gas(), address(), 0, 0, 0, 0)
        }
    }
}
