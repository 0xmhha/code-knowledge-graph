// W-C W6 V1.16 fixture — multi-return tuple destructuring (V0 scope:
// LHS explicit types per slot). `(uint256 a, address b) = foo();` —
// emit one local-var binding per typed slot. Then receiver `a` resolves
// to `uint256`, dispatched via SafeMath binding.
// Expectations:
//   - 1 EdgeUsesFor: Vault → SafeMath
//   - 1 EdgeCalls (V1.16 → V1.0): Vault.run → SafeMath.add

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 x, uint256 y) internal pure returns (uint256) {
        return x + y;
    }
}

contract Vault {
    using SafeMath for uint256;

    function pair() internal pure returns (uint256, address) {
        return (42, address(0));
    }

    function run() external pure returns (uint256) {
        (uint256 a, address b) = pair();
        b; // silence unused warning — not graph-relevant
        return a.add(1);
    }
}
