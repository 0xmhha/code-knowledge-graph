// W-C W6 V2.5 fixture — Sol 0.8.19+ user-defined operator binding via
// `using` directive. Grammar-level exploration to check whether
// tree-sitter-solidity v1.2.13 supports this syntax.
//
// V0 notes (queries.go preamble) marked free-function form
// (`using {f1, f2} for T`) as grammar-blocked (parses to ERROR for
// the `{Math.add, Math.sub}` brace shape). The user-defined operator
// form is a strict superset of that — uses the same `using_alias`
// node type with a `user_definable_operator` child.
//
// Pre-V2.5 query matches only `type_alias` (V0); `using_alias` is
// ignored. Therefore:
//   - If grammar parses cleanly → 0 EdgeUsesFor (V0 ignores
//     using_alias). V2.5 documents this as known partial support.
//   - If grammar parses with ERROR nodes → 0 EdgeUsesFor + ERROR
//     marker in AST. Same outcome from graph perspective.
//
// Either way, V2.5 locks the V0 behavior: operator-form `using`
// directives don't produce edges. V3+ would need to either upgrade
// the grammar or extend the query to match `using_alias` children.
//
// Expectations (current V0/V1/V2 behavior):
//   - 0 EdgeUsesFor from the operator-form directive.
//   - The function `mul` itself parses as NodeFunction (free
//     function or function inside contract), independent of the
//     using directive.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

function mul(uint256 a, uint256 b) pure returns (uint256) {
    return a * b;
}

// Operator-form: bind `mul` as the `*` operator for uint256.
// V0 query misses using_alias.
using {mul as *} for uint256 global;

contract Calc {
    function compute(uint256 x, uint256 y) external pure returns (uint256) {
        return x * y;
    }
}
