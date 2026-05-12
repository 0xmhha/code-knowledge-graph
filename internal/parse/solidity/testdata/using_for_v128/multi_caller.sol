// W-C W6 V1.28 fixture — multiple aliased imports + mixed bare/aliased
// usage. Confirms the per-file alias map handles multiple entries and
// preserves bare-name resolution alongside.
//
// Expectations:
//   - 2 EdgeUsesFor: Vault → SafeMath, Vault → Address
//   - 1 EdgeCalls: Vault.compute → SafeMath.add (via alias SM)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import {SafeMath as SM, Address as A} from "./multi_lib.sol";

contract Vault {
    using SM for uint256;
    using A for address;

    function compute(uint256 seed) external pure returns (uint256) {
        return seed.add(1);
    }
}
