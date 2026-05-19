// W-C W8 V5 fixture — function-typed state variable invocation.
//
// Sol allows declaring a function pointer as a state variable and
// invoking it as a bare identifier from any method on the same
// contract. Pre-V5 the W8 V3/V4 walker only matched callees against
// function-typed parameters and locals of the SAME callable; V5
// extends the match to the enclosing contract's function-typed
// state variables.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Hooked {
    // IsFunctionTyped=true on this NodeField (W8 V2).
    function(uint256) external returns (uint256) onAction;

    // V5: invokes `onAction` (a function-typed state-var) as a bare
    // identifier. HasFunctionPointerCall should be true.
    function trigger(uint256 x) external returns (uint256) {
        return onAction(x);
    }

    // V4 reference: no function-typed local or param, no state-var
    // invocation. HasFunctionPointerCall stays false.
    function passthrough(uint256 x) external pure returns (uint256) {
        return x;
    }
}
