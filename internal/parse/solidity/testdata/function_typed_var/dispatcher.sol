// W-C W8 V3 fixture — function-typed parameters and locals.
//
// The walker marks NodeFunction / NodeModifier with
// HasFunctionTypedVar=true when at least one parameter or local
// variable in the callable's signature or body is declared with a
// Solidity function type. Mirrors the W8 V2 detection on state vars
// but at callable scope.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Dispatcher {
    // HasFunctionTypedVar = true — function-typed parameter.
    function runWithCallback(
        function(uint256) external returns (uint256) cb,
        uint256 x
    ) external returns (uint256) {
        return cb(x);
    }

    // HasFunctionTypedVar = true — function-typed local variable.
    function pickAndRun(uint256 x) external returns (uint256) {
        function(uint256) external returns (uint256) local = chooseFn();
        return local(x);
    }

    // HasFunctionTypedVar = false — only primitive-typed locals / params.
    function plain(uint256 x) external pure returns (uint256) {
        uint256 y = x + 1;
        return y;
    }

    function chooseFn() internal pure returns (function(uint256) external returns (uint256)) {
        function(uint256) external returns (uint256) zero;
        return zero;
    }
}
