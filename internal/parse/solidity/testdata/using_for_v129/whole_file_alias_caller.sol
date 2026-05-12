// W-C W6 V1.29 fixture (whole-file alias caller).
// `import "./whole_file_alias_lib.sol" as L; using L.SafeMath for
// uint256;` — qualified library reference via namespace alias.
// Pre-V1.29 the using-for query captures only the first identifier
// of type_alias (the V0 single-identifier shape), so the qualified
// form drops or registers `L` as the library name (which doesn't
// exist as a library). V1.29 uses the LAST identifier of type_alias
// as the library name — namespace prefix is discarded because the
// global byName index already disambiguates by name + file.
//
// Expectations:
//   - 1 EdgeUsesFor: Vault → SafeMath (V1.29 last-id resolution)
//   - 1 EdgeCalls: Vault.compute → SafeMath.add

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "./whole_file_alias_lib.sol" as L;

contract Vault {
    using L.SafeMath for uint256;

    function compute(uint256 seed) external pure returns (uint256) {
        return seed.add(1);
    }
}
