// W-C W8 V8 fixture — function pointer propagation without
// invocation.
//
// Three cases the marker should distinguish:
//
//   - assignTo:    assigns a fn-typed param to a state var.
//                  HasFunctionPointerPropagation=true.
//                  HasFunctionPointerCall=false (no invocation).
//
//   - passThrough: forwards a fn-typed param to another function.
//                  HasFunctionPointerPropagation=true.
//                  HasFunctionPointerCall=false.
//
//   - invokeOnly:  invokes a fn-typed param. V4/V5 already mark
//                  HasFunctionPointerCall here; V8 keeps
//                  HasFunctionPointerPropagation=false because
//                  the call is invocation, not propagation.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Propagator {
    function(uint256) external returns (uint256) handler;

    function assignTo(function(uint256) external returns (uint256) cb) external {
        handler = cb;
    }

    function forwardArg(function(uint256) external returns (uint256) cb) external {
        register(cb);
    }

    function register(function(uint256) external returns (uint256) cb) internal {
        handler = cb;
    }

    function invokeOnly(function(uint256) external returns (uint256) cb, uint256 x) external returns (uint256) {
        return cb(x);
    }
}
