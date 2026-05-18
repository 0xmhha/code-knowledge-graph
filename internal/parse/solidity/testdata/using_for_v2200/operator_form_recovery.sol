// W-C W6 V2.20 fixture — operator-form using directive recovery.
//
// Sol 0.8.19+ `using {f as +} for T;` is parsed by vendored
// tree-sitter-solidity v1.2.11 as an ERROR-wrapped misparse: no
// `using_directive` node appears in the tree; the braced body is
// reinterpreted as a `state_variable_declaration` carrying the
// qualified function reference as a `user_defined_type` plus an
// `identifier "as"` and a trailing `ERROR` containing the operator
// and bound type (V2.17 AST probe evidence).
//
// V2.20 adds a recovery walker that pattern-matches that specific
// misparse shape and emits the same EdgeUsesFor pair runUsingFor
// produces, restoring the binding behavior the developer wrote.
//
// Three scopes covered in one fixture: contract, library, interface.
// Each emits 1 EdgeUsesFor (container → Math).

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

library Math {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

// Contract scope (V2.7 lock).
contract CContract {
    using {Math.add as +} for uint256;

    function f(uint256 x) external pure returns (uint256) { return x; }
}

// Library scope (V2.17 lock).
library CLibrary {
    using {Math.add as +} for uint256;

    function f(uint256 x) internal pure returns (uint256) { return x; }
}

// Interface scope (V2.14 IOp lock). Interfaces can't have function
// bodies; just declarations. The directive is semantically nonsensical
// (no state to bind methods on) but Sol allows it syntactically.
interface CInterface {
    using {Math.add as +} for uint256;
    function f(uint256 x) external pure returns (uint256);
}
