// W-C W6 V2.6 fixture — contract-scoped using-alias free-function form.
// `using {Math.add, Math.sub} for uint256;` — Solidity 0.8.13+ feature.
//
// Empirical finding (V2.6 probe): V0 queryUsingFor's
// `(using_directive (type_alias (identifier) @lib) ...)` matches this
// form despite the V0 spec's grammar-block claim. The parser produces
// a clean tree and the `Math` identifier inside the alias is captured
// as the `@lib` token. resolveUsingForRef then resolves Math against
// NodeContract (library) byName index and emits Calc → Math edge.
//
// Note: file-level using directives (`using ... for ... global;`) are
// NOT matched by queryUsingFor (intentional — V0 spec excludes file-
// level scope until grammar revisit). The V2.5 fixture with
// `using {mul as *} for uint256 global;` produced 0 edges for that
// reason, not because of any using_alias handling gap.
//
// Expectations:
//   - 1 EdgeUsesFor: Calc → Math (V0 incidental capture)
//   - Calc.compute body has no using-for dispatch (the using directive
//     binds methods but the function body itself doesn't use them).

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

library Math {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
    function sub(uint256 a, uint256 b) internal pure returns (uint256) {
        return a - b;
    }
}

contract Calc {
    using {Math.add, Math.sub} for uint256;

    function compute(uint256 x) external pure returns (uint256) {
        return x;
    }
}
