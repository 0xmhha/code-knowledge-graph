// W-C W6 V1.28 fixture (named-import alias caller).
// `import {SafeMath as SM} from "./aliased_lib.sol"; using SM for uint256;`
// — pre-V1.28 binding lookup tries to resolve SM as a library name,
// no library named SM exists (only SafeMath), so binding misses and
// dispatch drops. V1.28 adds a per-file alias map (SM → SafeMath) so
// the binding consults SafeMath instead.
//
// Expectations:
//   - 1 EdgeUsesFor: Vault → SafeMath (alias resolved)
//   - 1 EdgeCalls: Vault.compute → SafeMath.add

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import {SafeMath as SM} from "./aliased_lib.sol";

contract Vault {
    using SM for uint256;

    function compute(uint256 seed) external pure returns (uint256) {
        return seed.add(1);
    }
}
