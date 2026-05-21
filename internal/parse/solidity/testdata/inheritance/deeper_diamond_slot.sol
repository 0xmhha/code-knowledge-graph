// W-C W9 V20 fixture probe — deeper diamond storage slot.
//
// V18 covered the 3-node diamond (D ← {B, C} ← A): one shared
// base reached via two paths. V20 stresses C3 linearization on
// a *deeper* chain where the shared base is two levels up:
//
//   A
//   |
//   B          (B inherits A)
//  / \
// C   D       (C inherits B, D inherits B)
//  \ /
//   E         (E is C, D)
//
// MRO of E (Python-style C3):
//   E → C → D → B → A
//
// Reverse-MRO storage = [A, B, D, C, E]
//
//   - A's 1 slot:  slot 0
//   - B's 1 slot:  slot 1  (offset by A)
//   - D's 1 slot:  slot 2  (offset by A+B; D linearises before C)
//   - C's 1 slot:  slot 3  (offset by A+B+D)
//   - E's 1 slot:  slot 4  (offset by A+B+C+D = 4)
//
// What this tests beyond V18:
//   1. B is shared via TWO transitive paths (C→B and D→B).
//      A naive sum would double-count B, putting E.e at slot 5.
//   2. The MRO order (C before D in E's bases, but D before C
//      in the reverse-MRO storage walk) tests whether ckg
//      respects the C3 linearization ORDER, not just the
//      deduplicated SET.
//
// If the probe reports e=4 with proper offsets, V20 is a pure
// invariant lockdown — c3_linearization.go already handles the
// deeper case. If e>=5, the dedup is shallow and V20 turns into
// a fix on resolve.go's applyInheritanceStorageOffsets.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract DeepBaseA {
    uint256 public a;
}

contract DeepMidB is DeepBaseA {
    uint256 public b;
}

contract DeepLeftC is DeepMidB {
    uint256 public c;
}

contract DeepRightD is DeepMidB {
    uint256 public d;
}

contract DeepBottomE is DeepLeftC, DeepRightD {
    uint256 public e;
}
