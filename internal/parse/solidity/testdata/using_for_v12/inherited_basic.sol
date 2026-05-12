// W-C W6 V1.2 fixture — inherited using directive (single-level).
// Parent contract declares `using ParentLib for uint256;`. Child
// inherits without re-declaring; V1.2 must propagate the binding so
// child's state-var dispatch resolves through ParentLib.
// Expectations:
//   - 1 EdgeUsesFor: Parent → ParentLib (parent's own binding)
//   - 1 EdgeCalls: Child.bump → ParentLib.inc (state-var receiver
//     in Child picks up the inherited binding)
//
// Note: V0 EdgeUsesFor is NOT replicated for Child (we don't synthesise
// edges for inherited bindings — the binding map carries the dispatch
// semantics; the EdgeUsesFor remains on the parent that declared it).

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library ParentLib {
    function inc(uint256 self) internal pure returns (uint256) {
        return self + 1;
    }
}

contract Parent {
    using ParentLib for uint256;
}

contract Child is Parent {
    uint256 public value;

    function bump() external {
        value = value.inc();
    }
}
