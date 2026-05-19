// W-C W10 V10 fixture — Yul-level self-delegatecall.
//
// `delegatecall(gas(), address(), in, insize, out, outsize)` where
// `address()` is the Yul builtin returning the contract's own
// address is the Yul-level equivalent of
// `address(this).delegatecall(...)` — dead-weight re-execution
// of the contract's bytecode against its own storage. V10 marks
// HasSelfDelegatecallDead alongside the V3 HasLowLevelCall.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract YulSelfDelegate {
    function deadYulDelegate(bytes memory data) external returns (bool) {
        bool ok;
        assembly {
            let r := delegatecall(gas(), address(), add(data, 32), mload(data), 0, 0)
            ok := r
        }
        return ok;
    }

    function normalYulCall(address target, bytes memory data) external returns (bool) {
        bool ok;
        assembly {
            let r := call(gas(), target, 0, add(data, 32), mload(data), 0, 0)
            ok := r
        }
        return ok;
    }
}
