// W-C W6 V1.26 fixture — abstract contract with using-for binding and
// dispatch inside its body. abstract contract is the same AST kind as
// contract_declaration (tree-sitter v1.2.13 — verified via W4
// abstract_library.go); the `abstract` keyword surfaces in W4's
// SubKind on NodeContract. Verifies V1.x using-for indexing + dispatch
// is unchanged for abstract contracts.
//
// Expectations:
//   - 1 EdgeUsesFor: Base → SafeMath
//   - 1 EdgeCalls: Base.computeBase → SafeMath.add

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

abstract contract Base {
    using SafeMath for uint256;

    function computeBase(uint256 seed) internal pure returns (uint256) {
        return seed.add(1);
    }
}
