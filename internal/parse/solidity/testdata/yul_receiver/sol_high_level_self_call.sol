// W-C W10 V19 probe — high-level self-call variants.
//
// Solidity's "external call" boundary is at the *message call* level:
// `foo()` is an internal call (no message), `this.foo()` is an
// external call (full message, fresh execution context). The same
// re-entrancy invariant applies to *both* low-level
// `payable(this).call(...)` (V14-V18) and high-level `this.foo()`
// — the attacker can re-enter via the typed dispatch just as
// effectively as via raw bytes.
//
// Three syntax variants:
//
//   - ThisCall.fire       : this.target() — bare high-level self-call
//   - ContractCast.fire   : ThisCall(this).target() — wrap self in
//                           its own contract type before calling
//   - InterfaceCast.fire  : IFoo(this).foo() — wrap self in an
//                           interface type before calling

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

interface IFoo {
    function foo() external;
}

contract ThisCall is IFoo {
    function fire() external {
        this.target();
    }

    function target() external {}
    function foo() external override {}
}

contract ContractCast {
    function fire() external {
        ThisCall(address(this)).target();
    }
}

contract InterfaceCast is IFoo {
    function fire() external {
        IFoo(address(this)).foo();
    }

    function foo() external override {}
}
