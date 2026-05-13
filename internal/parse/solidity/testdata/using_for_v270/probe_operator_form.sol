// W-C W6 V2.7 fixture — contract-scope operator-form using directive.
// `contract C { using {Lib.f as +} for T; }` — Solidity 0.8.19+ feature.
//
// V2.5 covered the file-level operator-form (`using {f as *} for uint256
// global;` outside any contract body) and locked 0-edges due to
// queryUsingFor intentionally excluding file-level scope. V2.6 then
// rediscovered that contract-scope free-function form (`using {Math.add,
// Math.sub} for uint256;`) IS incidentally captured by V0 query — the
// `Math` identifier matches as the `@lib` token because tree-sitter
// v1.2.13 wraps the alias-list entry inside a `type_alias` node.
//
// V2.7 is the natural mirror question: does contract-scope operator-form
// (the `as +` variant) ALSO match incidentally, or does the
// user_definable_operator child change the AST shape enough that V0's
// `(type_alias (identifier) @lib)` no longer fits?
//
// V2.5 file-level + operator-form: 0 edges (scope excluded).
// V2.6 contract-scope + free-function: 1 edge (V0 incidental).
// V2.7 contract-scope + operator-form: ??? — this fixture pins it.
//
// The expectation is locked empirically by the paired test; this fixture
// is the probe input. If V0 matches it, that's another hidden capability
// (like V2.6). If V0 misses it, we document the operator-form ⇒ 0-edge
// behavior at contract scope and contrast it with V2.6.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

library Math {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract Calc {
    using {Math.add as +} for uint256;

    function compute(uint256 x) external pure returns (uint256) {
        return x + 1;
    }
}
