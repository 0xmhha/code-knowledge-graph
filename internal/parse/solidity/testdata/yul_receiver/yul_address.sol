// W-C W10 V4 fixture — address-typed Yul receiver.
//
// `target` is declared as `address` (not IImpl), so the W7.1
// resolveLowLevelCallRef chain finds the receiver type but the
// byName[NodeContract] / byName[NodeInterface] lookup misses
// ("address" isn't a contract / interface name). V3 marker fires
// for HasLowLevelCall; V4 adds HasExternalCall to distinguish
// arbitrary-address dispatch from resolved-receiver low-level calls.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Forwarder {
    address public target;

    function forward(bytes memory data) external returns (bool) {
        bool ok;
        assembly {
            let r := call(gas(), sload(0), 0, add(data, 32), mload(data), 0, 0)
            ok := r
        }
        return ok;
    }

    function forwardViaState(bytes memory data) external returns (bool) {
        address t = target;
        bool ok;
        assembly {
            let r := staticcall(gas(), t, add(data, 32), mload(data), 0, 0)
            ok := r
        }
        return ok;
    }
}
