// W-C W9 V15 fixture (file 2/2) — cross-file enum state-var.
//
// Holder imports status_lib.sol as the `Ext` namespace alias and
// declares Ext.Status as one of its state-vars. The W9 V14
// per-file enumSizes index only knows enums declared in the same
// file, so `Ext.Status` doesn't get the 1-byte packing treatment
// — it falls through to solTypeSize's conservative 32-byte
// fallback.
//
// Expected slot layout under V15 (audit-lock the gap):
//
//   head    : uint8       slot 0, byte 0   (packs)
//   status1 : Ext.Status  slot 1            (conservative full-slot)
//   tail    : uint256     slot 2            (uint256 advances)
//
// V16+ would index globalEnumSizes across all ParseResults and
// compress to:
//
//   head    : slot 0, byte 0
//   status1 : slot 0, byte 1
//   tail    : slot 1

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "./status_lib.sol" as Ext;

contract Holder {
    uint8       head;
    Ext.Status  status1;
    uint256     tail;
}
