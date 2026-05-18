// W-C W9 V1 fixture — inheritance offset on SlotIndex.
//
// V0 assigned per-contract local slot indices (each contract restarts
// at 0). V1 walks the EdgeExtends parent chain and adds ancestor
// slot counts so the SlotIndex reflects absolute EVM storage position.
//
// V1 scope: linear inheritance chains. Diamond inheritance with
// repeated ancestors (`contract D is Y, Z where Y is X and Z is X`)
// double-counts X under the naive sum used here; Sol's C3 linearization
// would dedupe X. Documented as V1 limitation.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract A {
    uint256 a;   // slot 0 (no parents → offset 0)
    uint256 b;   // slot 1
}

contract B is A {
    uint256 c;   // V0: 0; V1: 2 (offset = 2 from A)
}

contract C is B {
    uint256 d;   // V0: 0; V1: 3 (offset = 3 = A.2 + B.1)
}
