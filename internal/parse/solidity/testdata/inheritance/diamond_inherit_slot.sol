// W-C W9 V18 fixture probe — diamond inheritance storage slot.
//
// The diamond shape stresses C3 linearization on storage layout.
//
//   A   (uint256 a)
//   |\
//   B C (B adds uint256 b ; C adds uint256 c)
//    \|
//     D (uint256 d)
//
// Solidity rule: A appears once in D's MRO (the canonical C3
// dedup). Storage layout in reverse-MRO order = [A, B, C, D],
// so slots collapse to:
//
//   A.a → 0   (own-scope)
//   B.b → 1   (B's own slot 0 + A's 1-slot offset)
//   C.c → 1   (C's own slot 0 + A's 1-slot offset)
//   D.d → 3   (D's own slot 0 + A+B+C = 3-slot offset)
//
// The naive multi-base offset (= sum of base slot counts) would
// double-count A:
//
//   D.d → 4   (B reports 2 slots, C reports 2 slots → 2+2)
//
// Correct C3-aware offset:
//
//   D.d → 3   (A counted once: 1+1+1 = 3)
//
// W9 V1 + the c3_linearization.go module should produce 3; V18
// pins that. If the probe reports 4, V18 turns into a fix
// (resolve.go's applyInheritanceStorageOffsets needs MRO-aware
// dedup).

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract DiamondBaseA {
    uint256 public a;
}

contract DiamondLeftB is DiamondBaseA {
    uint256 public b;
}

contract DiamondRightC is DiamondBaseA {
    uint256 public c;
}

contract DiamondDerivedD is DiamondLeftB, DiamondRightC {
    uint256 public d;
}
