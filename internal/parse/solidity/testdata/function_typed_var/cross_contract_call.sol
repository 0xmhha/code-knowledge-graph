// W-C W8 V6 fixture — cross-contract function pointer invocation.
//
// Hub declares a function-typed state-var `onAction`. Caller holds
// a Hub instance via a state-var of contract type and invokes
// `h.onAction(x)`. Pass 2 resolves the receiver `h` to type Hub,
// finds Hub.onAction as a function-typed NodeField, and marks
// HasFunctionPointerCall on Caller.trigger.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Hub {
    function(uint256) external returns (uint256) onAction;

    function setHook(function(uint256) external returns (uint256) cb) external {
        onAction = cb;
    }
}

contract Caller {
    Hub h;

    // V6: cross-contract function-pointer invocation. Should mark
    // HasFunctionPointerCall=true.
    function trigger(uint256 x) external returns (uint256) {
        return h.onAction(x);
    }

    // Reference: no fn-pointer call. HasFunctionPointerCall stays
    // false.
    function noop() external pure {}
}
