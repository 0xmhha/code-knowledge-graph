// W-C W9 V28 fixture — struct-internal reference-type slot fallback (negative lock).
//
// `struct_size.go` V5 documents three categories of struct-member
// types as "use the conservative 32-byte slot" or "use the full-slot
// mapping advance":
//
//   - Mapping fields inside structs
//   - Dynamic-array fields inside structs (T[])
//   - Dynamic-string / dynamic-bytes fields inside structs
//
// Walker probe (2026-05-21) shows the V5 comment intent and the
// V5 walker behaviour diverge. tryComputeStructBytes uses three
// classification helpers — typeNameIsMapping (covers mapping),
// solFixedArrayBytes (fixed `T[N]` only), and solValueTypeSize
// (primitives + bytes1..bytes32 only) — and falls through
// `return 0, false` for every other type. Dynamic `bytes`,
// dynamic `string`, and dynamic `T[]` all hit the fallthrough.
// At fixed-point the affected struct stays out of `v.structSizes`
// entirely, so its state-var occurrence falls back to the
// conservative 1-slot path in runStateVarDecl — *not* to the
// per-member 32-byte conservative path V5 promised.
//
// Concretely: a `struct S { uint256 a; bytes b; uint256 c; }`
// declared as a state-var consumes 1 slot in V5's actual
// behaviour, not the 3 slots V5's comment implies (1 for each of
// uint256/bytes/uint256). The reference contract below makes the
// gap visible side-by-side.
//
// What the probe surfaced — and what V28 pins as characterization
// rather than expectation:
//
//   - V5's stated intent: a struct with one mapping / bytes /
//     dynamic-array member sizes to (member-count × full-slot).
//     For these three fixtures, that would mean 3 slots and
//     `tail` at slot 4.
//
//   - V5's actual behaviour: tryComputeStructBytes returns
//     (0, false) on unrecognised types, the struct stays out of
//     v.structSizes, and the state-var falls back to a 1-slot
//     conservative path. `tail` lands at slot 2.
//
//   - A second divergence surfaces in this fixture: even the
//     PrimitiveOnly contract — whose struct contains only uint256
//     members and would size cleanly in isolation (see V11
//     struct_deeply_nested.sol) — also lands on the 1-slot
//     fallback here. The all-primitive baseline does *not*
//     produce the 3-slot layout V5's helpers would in V11's
//     setup. Root cause (multi-contract namespace collision on
//     `structSizes["S"]`, qualified-vs-unqualified state-var
//     lookup, or fixed-point ordering) is unidentified at V28
//     time and out of scope for V28 itself.
//
// V28 records the full 4-contract grid landing on the same
// fallback so a future walker fix is forced to consider the whole
// picture: did it resolve only the reference-type member handling?
// Did it also fix multi-contract resolution? The cross-flip
// protocol prevents a half-fix from shipping silently.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract PrimitiveOnly {
    struct S {
        uint256 a;
        uint256 b;
        uint256 c;
    }
    // Resolvable: S sizes to 3 slots.

    uint8 head;       // slot 0
    S inner;          // starts slot 1, takes 3 slots: 1, 2, 3
    uint8 tail;       // slot 4
}

contract WithMap {
    struct S {
        uint256 a;
        mapping(uint => uint) m;
        uint256 b;
    }
    // V5 unresolvable: typeNameIsMapping handles mapping but the
    // surrounding tryComputeStructBytes path does not advance
    // through it cleanly when the surrounding fixed-point also
    // has other unresolvable members.

    uint8 head;       // slot 0
    S inner;          // slot 1, fallback 1-slot
    uint8 tail;       // slot 2 (V5 fallback) — would be slot 4 if V5 comment intent landed
}

contract WithBytes {
    struct S {
        uint256 a;
        bytes b;      // V5 unresolvable: solValueTypeSize rejects "bytes"
        uint256 c;
    }

    uint8 head;       // slot 0
    S inner;          // slot 1
    uint8 tail;       // slot 2
}

contract WithDynArray {
    struct S {
        uint256 a;
        uint256[] b;  // V5 unresolvable: solFixedArrayBytes rejects "uint256[]"
        uint256 c;
    }

    uint8 head;       // slot 0
    S inner;          // slot 1
    uint8 tail;       // slot 2
}
