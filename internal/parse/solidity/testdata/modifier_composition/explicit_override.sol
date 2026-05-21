// W-C W7.4 V25 fixture — explicit-list modifier override.
//
// W7.3 V0 (order_and_override.sol) locked *bare* `modifier m() override`.
// `runModifierOverride` (overrides.go) has a sibling branch that handles
// the explicit-list form `modifier m() override(A, B) { _; }` and emits
// one EdgeOverrides PendingRef per listed parent with
// DispatchKind=dispatchKindOverrideExplicit. That branch is *code*
// without a *test* — exactly the silent-regression shape the
// WALKER_SYMMETRY drift catalogue warns about (cross-walker symmetry,
// here mirroring W2 function-override explicit-list).
//
// Layout: two abstract parents each declare the same modifier name
// (`m`) as virtual; child contract diamond-inherits from both and
// resolves the conflict with an explicit `override(A, B)` list. Per
// `runModifierOverride`, this must emit exactly two EdgeOverrides
// edges from `Child.m` — one to `A.m`, one to `B.m`.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

abstract contract A {
    modifier m() virtual { _; }
}

abstract contract B {
    modifier m() virtual { _; }
}

contract Child is A, B {
    // Multi-parent override resolution: explicit-list form. Solidity
    // requires this when two unrelated bases declare the same virtual
    // modifier and the child re-declares it. Each listed parent
    // should produce its own EdgeOverrides edge.
    modifier m() override(A, B) { _; }

    function guarded() external m {}
}
