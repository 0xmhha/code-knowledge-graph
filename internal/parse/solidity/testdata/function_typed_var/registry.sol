// W-C W8 V2 fixture — function-typed state variables.
//
// V0 marks NodeField with IsFunctionTyped=true when the field's
// declared type is a Solidity function type. Detection is shape-
// based: the state_variable_declaration's type_name child has a
// `parameter` (or `return_parameter`) child instead of the usual
// primitive_type / user_defined_type / mapping shape.
//
// Call-site resolution (`handler(x)` → which function value) is
// deferred. V0 surfaces only the field-declaration evidence.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Registry {
    // IsFunctionTyped = true
    function(uint256) external returns (uint256) handler;

    // IsFunctionTyped = true
    function(address, uint256) external payable callback;

    // IsFunctionTyped = false — plain primitive.
    uint256 counter;

    // IsFunctionTyped = false — user-defined type.
    address owner;
}
