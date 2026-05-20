// W-C W8 V14 fixture — function-of-function-pointer (nested
// return-type fn pointer).
//
//   - higherOrder is a state-var whose type is itself a function
//     pointer whose RETURN type is another function pointer:
//     `function(uint256) internal pure returns (function(uint256)
//      internal pure returns (uint256))`.
//     V2's IsFunctionTyped marker should still fire because the
//     outer type_name has parameter/return_parameter children
//     directly — the nested fn-typed return doesn't shadow that
//     signal.
//   - withNestedLocal declares the same type as a local. V3's
//     HasFunctionTypedVar should fire on the enclosing function.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Nested {
    // IsFunctionTyped=true (outer fn-pointer, return type is also fn-typed).
    function(uint256) internal pure returns (function(uint256) internal pure returns (uint256)) higherOrder;

    function withNestedLocal() internal view returns (uint256) {
        // HasFunctionTypedVar=true on the enclosing function.
        function(uint256) internal pure returns (function(uint256) internal pure returns (uint256)) local = higherOrder;
        return local(0)(1);
    }
}
