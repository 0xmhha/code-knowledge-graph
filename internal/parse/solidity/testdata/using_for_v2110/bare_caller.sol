// W-C W6 V2.11 fixture — bare path-only import (Solidity default
// `import "./path.sol";` form). This is the last import shape after
// V1.28 named alias / V1.29 whole-file namespace alias / V2.10 mixed
// bare-aliased multi-import unification.
//
// Semantics: the bare path-only form brings ALL top-level
// declarations of the target file into the importing file's local
// namespace, accessible by their original names (Solidity 0.7+ spec).
// In our parse pipeline this means:
//
//   - No entry recorded in importAliases (no `as Alias` clause).
//   - No entry recorded in namespaceAliases (no `as L` namespace).
//   - runUsingFor sees the bare identifier `SafeMath` and goes
//     straight to byName[NodeContract]["SafeMath"] which finds the
//     library declared in the partner file via the global
//     post-parse index.
//
// V1.14 covered cross-file struct-field receiver dispatch using the
// global byName index. V2.11 confirms the same global index path
// works for the most pedestrian import form — the one Solidity
// developers actually use most of the time.
//
// Expectations:
//   - 1 EdgeUsesFor: Vault → SafeMath (cross-file, ConfInferred).
//   - 1 EdgeCalls: Vault.compute → SafeMath.add (V1.0 dispatch).
//   - Surround-safety: SafeMath, SafeMath.add, Vault, Vault.compute
//     all index normally.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "./bare_libs.sol";

contract Vault {
    using SafeMath for uint256;

    function compute(uint256 seed) external pure returns (uint256) {
        return seed.add(1);
    }
}
