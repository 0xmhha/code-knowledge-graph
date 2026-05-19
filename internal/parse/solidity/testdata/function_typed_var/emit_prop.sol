// W-C W8 V10 fixture — emit-statement propagation.
//
//   - logHandler: emits an event with a fn-typed state-var as
//                  argument. V10 marks
//                  HasFunctionPointerPropagation=true.
//                  HasFunctionPointerCall stays false (no
//                  invocation).
//
//   - logPlain:   emits an event with a primitive argument; no
//                  propagation marker.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract EventEmitter {
    function(uint256) external returns (uint256) handler;

    event HandlerRegistered(function(uint256) external returns (uint256) cb);
    event Plain(uint256 value);

    function logHandler() external {
        emit HandlerRegistered(handler);
    }

    function logPlain(uint256 x) external {
        emit Plain(x);
    }
}
