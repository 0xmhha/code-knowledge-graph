// W-C W7.1 V0 fixture — low-level call dispatch via state-var receiver.
//
// Three primitives detected:
//   - target.call(data)         → EdgeInvokes AMBIGUOUS
//   - target.delegatecall(data) → EdgeInvokes AMBIGUOUS
//   - target.staticcall(data)   → EdgeInvokes AMBIGUOUS
//
// V0 receiver resolution: state-var name → declared type (IFoo) →
// byName[NodeInterface] lookup → IFoo node ID as Dst.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

interface IFoo {
    function bar() external;
}

contract Proxy {
    IFoo public target;

    function viaCall(bytes memory data) external returns (bool) {
        (bool ok, ) = target.call(data);
        return ok;
    }

    function viaDelegatecall(bytes memory data) external returns (bool) {
        (bool ok, ) = target.delegatecall(data);
        return ok;
    }

    function viaStaticcall(bytes memory data) external view returns (bool) {
        (bool ok, ) = target.staticcall(data);
        return ok;
    }
}
