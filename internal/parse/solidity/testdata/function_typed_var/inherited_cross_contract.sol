// W-C W8 V7 fixture — inherited function-typed state-var call.
//
// Base declares `onAction` as a function-typed state-var. Hub
// extends Base but doesn't declare any new field. Caller holds a
// Hub instance and invokes `h.onAction(x)`. Pre-V7 the marker
// missed because fnTypedFields lookup checked only Hub directly,
// not Base. V7 walks Hub's C3 MRO so Base.onAction is reachable.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Base {
    function(uint256) external returns (uint256) onAction;
}

contract Hub is Base {}

contract Caller {
    Hub h;

    // V7: inherited fn-typed state-var invocation.
    // HasFunctionPointerCall should be true.
    function trigger(uint256 x) external returns (uint256) {
        return h.onAction(x);
    }
}
