// W-C W6 V1.29 fixture (collision risk side). Unrelated contract
// named "L" — same identifier as the whole-file alias used in
// whole_file_alias_caller.sol. If runUsingFor emits a PendingRef for
// the namespace-alias prefix L, Pass 2 byName[NodeContract] lookup
// could surface L (this contract) as the resolved library → false-
// positive EdgeUsesFor (Vault → L).

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract L {
    // Empty body — purely a name collision target.
    uint256 public unused;
}
