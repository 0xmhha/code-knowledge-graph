// W-C W9 V6 fixture (consumer half) — uses the Inner struct defined
// in shapes.sol as a state variable. Pre-V6 the holder's SlotIndex
// computation didn't know Inner occupies 2 slots and fell back to
// the conservative 32-byte (1 slot) advance, mis-placing the
// trailing fields.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "./shapes.sol";

contract Holder {
    uint8 head;     // slot 0  used 1
    Inner inner;    // slot 1..2  (cross-file struct -> 2 slots in V6)
    uint8 middle;   // slot 3  (after struct, new slot)
    Inner second;   // slot 4..5
    uint8 tail;     // slot 6
}
