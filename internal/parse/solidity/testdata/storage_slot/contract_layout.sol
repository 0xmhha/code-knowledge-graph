// W9 V0 fixture — per-contract slot index for state-vars.
//
// V0 scope: declaration-order index, no packing, no inheritance offset.
// Mapping nodes (NodeMapping) skip slot assignment — they live on a
// separate emit path and slot derivation is dynamic (keccak-based).

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Layout {
    uint256 public totalSupply;            // SlotIndex = 0
    address public owner;                  // SlotIndex = 1
    uint8 public decimals;                 // SlotIndex = 2 (V0: ignores packing)
    mapping(address => uint256) balances;  // NodeMapping — no slot
    bool public paused;                    // SlotIndex = 3
}

contract Other {
    uint256 a;   // SlotIndex = 0 (per-contract, restarts)
    uint256 b;   // SlotIndex = 1
}
