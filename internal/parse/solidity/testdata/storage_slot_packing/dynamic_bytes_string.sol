// W-C W9 V12 fixture — dynamic bytes / string slot occupancy.
//
// Sol's `bytes` and `string` types each occupy exactly one
// storage slot at the declaration site regardless of runtime
// content length. Short values (≤31 bytes) inline into the slot
// (length-in-low-byte + content); long values store length in
// the slot and content at keccak256(slot). Either way the slot
// index calculation treats them as a single full slot.
//
// Expected layout:
//   head    : uint8         slot 0
//   name    : string        slot 1 (full slot — dynamic)
//   payload : bytes         slot 2 (full slot — dynamic)
//   value   : uint256       slot 3
//   suffix  : uint8         slot 4

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract DynamicBytes {
    uint8   head;
    string  name;
    bytes   payload;
    uint256 value;
    uint8   suffix;
}
