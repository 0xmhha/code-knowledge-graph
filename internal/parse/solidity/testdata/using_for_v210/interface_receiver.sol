// W-C W6 V2.1 fixture — interface as bound type. Common DeFi pattern:
// state-var typed as an interface, with `using HelperLib for IToken;`.
// The receiver `token.helper()` dispatches via the bound library which
// takes the interface as its first parameter.
//
// Verifies that V0/V1.0 binding map handles interface-typed receivers
// uniformly with struct/primitive-typed receivers — typeName is just
// a string key, and byName indexing (Pass 2) uses NodeInterface for
// interface declarations.
//
// Expectations:
//   - 1 EdgeUsesFor: Vault → HelperLib (binding IToken → HelperLib)
//   - 1 EdgeCalls (V1.0 state-var receiver): Vault.run → HelperLib.balanceOf

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

interface IToken {
    function balanceOf(address holder) external view returns (uint256);
}

library HelperLib {
    function balanceOf(IToken t) internal view returns (uint256) {
        return t.balanceOf(msg.sender);
    }
}

contract Vault {
    using HelperLib for IToken;

    IToken public token;

    function run() external view returns (uint256) {
        return token.balanceOf();
    }
}
