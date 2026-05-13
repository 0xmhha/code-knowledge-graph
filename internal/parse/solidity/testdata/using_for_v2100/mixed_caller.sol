// W-C W6 V2.10 fixture — mixed bare + aliased entries in a single
// import statement. Exercises V1.28 importAliases handling for the
// case where ONE entry has `as Alias` and another does NOT.
//
// V1.28's TestUsingForV128_MultiAliasedImport already covered the
// all-aliased variant (`import {SafeMath as SM, Address as A}`).
// V1.28's TestUsingForV128_AliasedNamedImport covered the single-
// aliased variant. The mixed (heterogeneous) variant has been a
// blind spot: the walker iterates positional `alias` / `import_name`
// fields per entry, and a missing `alias` field on the bare entry
// must NOT spuriously map the bare name to anything (or trigger an
// off-by-one with the aliased entry).
//
// Expectations:
//   - 2 EdgeUsesFor: Vault → SafeMath (bare), Vault → Address
//     (resolved via alias A)
//   - 1 EdgeCalls: Vault.compute → SafeMath.add (bare path)
//   - Vault.zero compiles & indexes; the `addr.isZero()` call may
//     produce a separate EdgeCalls but is not the V2.10 assertion.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import {SafeMath, Address as A} from "./mixed_libs.sol";

contract Vault {
    using SafeMath for uint256;
    using A for address;

    function compute(uint256 seed) external pure returns (uint256) {
        return seed.add(1);
    }

    function zero(address addr) external pure returns (bool) {
        return addr.isZero();
    }
}
