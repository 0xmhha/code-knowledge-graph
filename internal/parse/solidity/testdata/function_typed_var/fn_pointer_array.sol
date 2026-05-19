// W-C W8 V12 fixture — function pointer array.
//
//   - handlers is a fn-typed state-var array. IsFunctionTyped
//     fires on the NodeField (V2/V12). HasFunctionTypedVar on
//     the contract's containing functions only fires for those
//     that declare such an array as param/local — captureArray
//     declares a local that the V12 detector should pick up via
//     the array_type recursion.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract ArrayHolder {
    // IsFunctionTyped=true (V12: array_type recursion).
    function(uint256) external returns (uint256)[] handlers;

    // HasFunctionTypedVar=true (V12: local is array-of-fn).
    function captureArray() external view returns (uint256) {
        function(uint256) external returns (uint256)[] memory local = handlers;
        return local.length;
    }
}
