// W-C W10 V24 fixture — high-level self-call × callable shape cross-axis.
//
// WALKER_SYMMETRY.md flagged three '?' cells: the high-level walker
// (V19) shares nearestFunctionQnameAndStart with the cast walker, so
// every callable shape the cast walker recognises *should* fire
// HasHighLevelSelfCall transparently when the body contains a typed
// self-call. V14 / V15 / V17 locked the cast walker side; V20 locked
// the try-wrapped variant. V24 closes the rest in one fixture.
//
// Three contracts exercise the three remaining shapes:
//
//   - CtorHighSelf.constructor  : this.target() inside a payable
//     constructor body. V15 already locked the cast walker on
//     constructors; V19's marker is expected to propagate via the
//     same nearestFunctionQnameAndStart path.
//
//   - FallbackHighSelf.receive  : this.target() inside receive()
//     payable. V14 locked the cast walker on receive/fallback.
//
//   - ModifierHighSelf.guard    : this.target() inside a modifier
//     body. V17 locked the cast walker on modifier scope; the
//     high-level walker should set HasHighLevelSelfCall on the
//     NodeModifier row in the same way V17 set
//     HasSelfReentrantCall there.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract CtorHighSelf {
    constructor() payable {
        this.target();
    }

    function target() external payable {}
}

contract FallbackHighSelf {
    receive() external payable {
        this.target();
    }

    function target() external payable {}
}

contract ModifierHighSelf {
    modifier guard() {
        _;
        this.target();
    }

    function protected() external guard {}
    function target() external {}
}
