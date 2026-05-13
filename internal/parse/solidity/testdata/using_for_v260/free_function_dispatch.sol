// W-C W6 V2.6 fixture — contract-scoped using-alias with body dispatch.
// Builds on probe_using_alias.sol by adding receiver method calls so
// we verify V1.0 + V2.x dispatch chains work end-to-end through the
// using_alias form.
//
// Expectations:
//   - 1 EdgeUsesFor: Vault → Math (binding via using-alias)
//   - 1 EdgeCalls (V1.0 state-var receiver via using-alias binding):
//     Vault.run → Math.add
//
// V0 query captures the `@lib` identifier from `{Math.add, Math.sub}`
// (the `Math` part). V1.0+ dispatch then resolves `x.add(...)` via
// bindings[Vault]["uint256"] → Math → Math.add.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

library Math {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
    function sub(uint256 a, uint256 b) internal pure returns (uint256) {
        return a - b;
    }
}

contract Vault {
    using {Math.add, Math.sub} for uint256;

    uint256 public x;

    function run() external view returns (uint256) {
        return x.add(1);
    }
}
