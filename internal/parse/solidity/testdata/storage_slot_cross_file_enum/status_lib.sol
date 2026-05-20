// W-C W9 V15 fixture (file 1/2) — external enum definition. The
// Status enum has 3 variants so under same-file V14 packing it
// would consume 1 byte. The companion holder.sol file imports
// this file as a namespace alias and declares a Status-typed
// state-var.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

enum Status {
    Active,
    Paused,
    Closed
}
