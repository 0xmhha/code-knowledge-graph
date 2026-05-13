// W-C W6 V2.4 fixture (cross-file V2.2 multi-binding caller).
// `using LibA for uint256; using LibB for uint256;` — V2.2 multi-
// binding extension verified across file boundary. Vault.run uses
// both LibA.tag and LibB.bump in sequence. resolveBindingLib should
// pick the right library per method-name across the multi-value
// binding list.
//
// Expectations:
//   - 2 EdgeUsesFor: Vault → LibA, Vault → LibB (cross-file ConfInferred)
//   - 1 EdgeCalls: Vault.run → LibA.tag
//   - 1 EdgeCalls: Vault.run → LibB.bump
//   (both at ConfInferred since libraries cross-file)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Vault {
    using LibA for uint256;
    using LibB for uint256;

    function run() external pure returns (uint256) {
        uint256 x = 1;
        x = x.tag();   // LibA.tag (unique to LibA)
        x = x.bump();  // LibB.bump (unique to LibB)
        return x;
    }
}
