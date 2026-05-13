// W-C W6 V2.9 fixture — contract-scope bare free-function alias.
// `using {addPlusOne} for uint256;` — Solidity 0.8.13+ feature
// (free-function form, no library qualifier).
//
// V2.6 / V2.7 / V2.9 form an alias-shape triplet at contract scope:
//
//   alias-entry shape                  | example                | V?
//   -----------------------------------+------------------------+-----
//   library-qualified (Lib.member)     | {Math.add, Math.sub}   | V2.6: 1 edge
//   operator-form (Lib.member as +)    | {Math.add as +}        | V2.7: 0 edges
//   bare (free-function name only)     | {addPlusOne}           | V2.9: this
//
// V0 query (`(type_alias (identifier) @lib)`) was empirically shown
// in V2.6 to match library-qualified entries because tree-sitter
// v1.2.13's AST wraps `Lib.member` such that `Lib` appears as
// `(type_alias (identifier))`. In V2.7 the `as +` operator suffix
// changed the alias-entry node from `type_alias` to `using_alias`
// (with a `user_definable_operator` child), losing the V0 match.
//
// V2.9 asks: what about the bare form (no `.` qualifier, no `as`)?
// Two AST hypotheses:
//   (H1) The bare form also produces `type_alias` wrapping →
//        V0 matches `addPlusOne` as @lib token, but the byName
//        index lookup (which expects a Contract/library) fails →
//        PendingRef remains, no EdgeUsesFor emitted → 0 edges.
//   (H2) The bare form produces a different AST node (e.g.
//        `using_alias` without operator) → V0 doesn't match at
//        all → 0 edges from a different mechanism.
//
// Either hypothesis predicts 0 EdgeUsesFor. The test below locks
// that outcome regardless of which AST mechanism applies.
//
// V1.0+ dispatch chain follow-up: even if bindings were populated
// (hypothetically), V1.0's resolveBindingLib looks up `lib.method`
// in funcByQName. For bare aliases there is no library — the
// "lib name" would be the function name itself, and
// `addPlusOne.addPlusOne` won't match anything. So 0 EdgeCalls
// from the bare alias path as well.
//
// Expectations:
//   - 0 EdgeUsesFor for the bare free-function alias.
//   - The free function `addPlusOne` itself indexes (NodeFunction
//     at file scope, qname = "addPlusOne").
//   - The contract `Calc` and its function `compute` index.
//   - No EdgeCalls from `Calc.compute` to `addPlusOne` via using-
//     for (the receiver method call `.addPlusOne()` may surface
//     as an unresolved call but should not produce a using-for-
//     mediated EdgeCalls).

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

// Free function (Solidity 0.7+) — module-scope, no enclosing library.
function addPlusOne(uint256 a) pure returns (uint256) {
    return a + 1;
}

contract Calc {
    using {addPlusOne} for uint256;

    function compute(uint256 x) external pure returns (uint256) {
        return x.addPlusOne();
    }
}
