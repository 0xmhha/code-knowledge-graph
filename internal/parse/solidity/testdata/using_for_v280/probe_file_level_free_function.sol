// W-C W6 V2.8 fixture — file-level free-function form using directive.
// `using {Lib.f} for T global;` — Solidity 0.8.13+ feature at module
// scope (outside any contract / library / interface body).
//
// This is the 2x2 matrix's last quadrant:
//   (scope × alias-shape)
//                | free-function    | operator-form
//   -------------+------------------+------------------
//   file-level   | V2.8 (this)      | V2.5: 0 edges
//   contract-sc. | V2.6: 1 edge     | V2.7: 0 edges
//
// V0 queryUsingFor's three top-level alternatives all match inside
// `(contract_body ...)` only — `contract_declaration`,
// `library_declaration`, `interface_declaration`. File-level
// `using_directive` (a direct child of `source_file`) is therefore
// outside every alternative's container, so V0 should miss it
// regardless of the alias-entry shape (`type_alias` vs `using_alias`).
//
// Additionally, V0 + V1.2 grammar limitation note (queries.go preamble,
// 2026-05-12) documented that tree-sitter-solidity v1.2.13's parser
// "wraps such directives in ERROR nodes." V2.8 empirically checks
// whether that ERROR cascade contaminates surrounding declarations —
// V2.5's file-level operator-form fixture already showed the answer
// was no (surround-safety held for `mul`, `Calc`, `Calc.compute`).
// V2.8 confirms the same holds for the free-function variant.
//
// Expectations:
//   - 0 EdgeUsesFor from the file-level directive (V0 scope exclusion).
//   - The library `Math`, function `Math.add`, contract `Calc`, and
//     function `Calc.compute` all index normally (no ERROR cascade).

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

library Math {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

// File-level free-function form: applies the binding globally to the
// `uint256` type across this compilation unit. The `global` keyword is
// required for file-level using directives.
using {Math.add} for uint256 global;

contract Calc {
    function compute(uint256 x) external pure returns (uint256) {
        return x;
    }
}
