// W-C W6 V2.14 fixture — interface-body using-for, three variants.
//
// V0 query (queries.go §W6) nests `using_directive (type_alias ...)`
// under `interface_declaration`, so interface bodies ARE walked. V2.14
// asks the empirical question: when `using X for T;` appears inside
// an interface body — where the directive is semantically nonsensical
// since interfaces have no state and no implementations to bind methods
// on — what does the parser+resolver emit?
//
// Three variants in one fixture (one interface per variant):
//
//   IBare → `using SafeMath for uint256;`         (legacy type-alias)
//   IFree → `using {Math.add} for uint256;`       (0.8.13+ free-func)
//   IOp   → `using {Math.add as +} for uint256;`  (0.8.19+ operator)
//
// Pre-RED hypothesis (lock or fix decided after first run):
//
//   IBare → 1 EdgeUsesFor : V0 happy path matches the type_alias
//                           identifier inside interface_declaration.
//   IFree → 1 EdgeUsesFor : V2.6-style incidental capture — the
//                           qualified alias entry `Math.add` exposes
//                           `Math` as `@lib` via the type_alias child.
//   IOp   → 0 EdgeUsesFor : V2.7-style AST shape mismatch — the
//                           `as +` operator alias wraps the entry in
//                           `user_definable_operator`, breaking the
//                           type_alias match path.
//
// Surround-safety: SafeMath, Math, IBare, IFree, IOp must all index.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

library Math {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

interface IBare {
    using SafeMath for uint256;
    function compute(uint256 x) external view returns (uint256);
}

interface IFree {
    using {Math.add} for uint256;
    function compute(uint256 x) external view returns (uint256);
}

interface IOp {
    using {Math.add as +} for uint256;
    function compute(uint256 x) external view returns (uint256);
}
