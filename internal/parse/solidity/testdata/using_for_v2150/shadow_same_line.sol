// W-C W6 V2.15 fixture — same-line shadow disambiguation.
//
// V2.0 introduced line-range scope-aware localVar lookup. The carry-
// over (V2.1+) flagged: "byte 정밀도 (column-based scope) — V2.0
// line-range refinement, >1 stmt per line edge cases".
//
// This fixture compresses V2.0's shadow_inner_use.sol function body
// onto a single line so that:
//
//   outer x  (uint256) declLine = scopeEndLine = L
//   inner x  (bytes32) declLine = scopeEndLine = L
//   inner use x.tag(...)                    line = L
//   outer use x.add(1)                      line = L
//
// V2.0 selectLocalDecl filter: declLine ≤ useSiteLine ≤ scopeEndLine.
// Both decls pass for both use sites (everything on line L). The
// tiebreak `decls[i].declLine > decls[bestIdx].declLine` is strict
// `>`, so the *first-appended* decl wins — that's the outer (parsed
// first). Outcome under V2.0:
//   - inner use `x.tag(...)` → resolves to outer uint256 → Other has
//     no `tag` on uint256 → edge dropped (FALSE NEGATIVE).
//   - outer use `x.add(1)`   → resolves to outer uint256 → SafeMath
//     has `add` on uint256 → edge emitted (lucky correct).
//
// V2.15 expectation (byte-offset disambiguation):
//   - 2 EdgeCalls (both restored):
//       C.f → Other.tag      (inner scope)
//       C.f → SafeMath.add   (outer scope)
//   - 2 EdgeUsesFor (unchanged): C → SafeMath, C → Other
//
// RED on first run (V2.0 line-only): 1 EdgeCalls only (SafeMath.add).
// V2.15 fix: byte-offset scope containment + max declStartByte tiebreak
// → inner use picks inner shadow, outer use picks outer.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

library Other {
    function tag(bytes32 a, bytes32 b) internal pure returns (bytes32) {
        return a ^ b;
    }
}

contract C {
    using SafeMath for uint256;
    using Other for bytes32;

    function f(bool cond) external pure returns (uint256) { uint256 x = 1; if (cond) { bytes32 x = bytes32(0); x.tag(bytes32(0)); } return x.add(1); }
}
