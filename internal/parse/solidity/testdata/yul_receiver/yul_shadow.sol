// W-C W10 V3 fixture — Yul let-binding shadow guard.
//
// Without the V3 shadow guard, the V2 walker would extract `impl`
// from the yul_path leading-identifier rule and resolve it against
// the Sol scope state-var `impl` of type IImpl, emitting an
// EdgeInvokes to IImpl. With the guard, the local `let impl := ...`
// declaration shadows the Sol identifier inside the assembly body,
// and the emit is skipped. The function still gets
// HasLowLevelCall=true via the V3 marker.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

interface IImpl {
    function run(bytes calldata data) external returns (bytes memory);
}

contract Proxy {
    IImpl public impl;

    function delegateShadowed(bytes memory data, address target) external returns (bool) {
        bool ok;
        assembly {
            // Local `impl` shadows the Sol-scope `impl` state variable.
            let impl := target
            let r := delegatecall(gas(), impl, add(data, 32), mload(data), 0, 0)
            ok := r
        }
        return ok;
    }
}
