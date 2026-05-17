// W-C W6 V2.17 fixture — library-scope operator-form using directive.
//
// V2.16 row 2 RECLASSIFICATION (not closure): empirical AST dump
// 2026-05-17 against vendored tree-sitter-solidity v1.2.11 revealed
// that operator-form `using {f as +} for T;` is grammar-rejected, not
// just query-gapped. The braced operator-form body is misclassified
// as a state_variable_declaration wrapped in ERROR nodes, with no
// using_directive node at all. V0 query cannot match what the parser
// never produces.
//
// Scope coverage matrix for operator-form:
//   V2.5  file-level     × operator-form → 0 edges (grammar-blocked
//                                          at file-level scope per
//                                          V2.16 row 1 — independent
//                                          of operator-form gap).
//   V2.7  contract-scope × operator-form → 0 edges (grammar reject).
//   V2.14 interface IOp  × operator-form → 0 edges (grammar reject).
//   V2.17 library-scope  × operator-form → 0 edges (NEW cell — this
//                                          fixture locks the
//                                          grammar-block at library
//                                          scope, the only non-file
//                                          scope previously untested).
//
// Why library scope specifically: V2.5/V2.7/V2.14 covered file /
// contract / interface respectively; library scope was untested for
// operator-form. V2.17 confirms the same grammar-block applies
// uniformly to library bodies.
//
// Future-proofing: when the vendored grammar upgrades to a version
// that parses operator-form correctly, this test will fail with
// got=1, forcing the V2.x stack to coordinate a lock-flip across
// V2.7 / V2.14 IOp / V2.17 simultaneously.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

library Math {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

library OpHelpers {
    using {Math.add as +} for uint256;

    function combine(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}
