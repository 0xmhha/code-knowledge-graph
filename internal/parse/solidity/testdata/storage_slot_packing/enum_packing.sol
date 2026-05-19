// W-C W9 V13 fixture — enum slot occupancy.
//
// Sol enums with ≤256 variants are stored as uint8 (1 byte) at
// runtime. solTypeSize's fallback treats unknown type names as
// 32 bytes (full slot), so consecutive enum fields currently do
// NOT pack — each one gets its own slot. V13 audit locks the
// conservative current behaviour. Promoting to actual enum-size
// packing requires a Pass-2 enum-size index (W9 V14+ scope).
//
// Expected slot layout under the current conservative model:
//
//   head  : uint8    slot 0
//   role1 : Role     slot 1   (would pack with head in V14+)
//   role2 : Role     slot 2   (would pack in V14+)
//   tail  : uint256  slot 3
//
// V14+ ideal (with proper enum size lookup) would compress to
// 2 slots: { head, role1, role2 } share slot 0, tail at slot 1.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract EnumHolder {
    enum Role { Reader, Writer, Admin }

    uint8   head;
    Role    role1;
    Role    role2;
    uint256 tail;
}
