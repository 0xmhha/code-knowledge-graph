// W-C W6 V1.27 fixture — Parent contract declares a modifier that uses
// the contract's `using SafeMath for uint256` binding inside its body.
// Child inherits Parent. Modifier resolution mechanics:
//   - Parent.check is V1.22-emitted with qname "Parent.check".
//   - Inside Parent.check body, `amount.add(0)` dispatches via
//     containerIDByFuncID[Parent.check ID] → Parent → bindings[Parent]
//     → SafeMath. (V1.22 + V1.0 path.)
// Confirms that having a child inherit Parent doesn't perturb the
// modifier's own dispatch resolution — bindings[Parent] stays intact.
//
// Expectations:
//   - 2 EdgeUsesFor (Parent → SafeMath + V1.2 inherited Child → SafeMath
//     — V1.2 propagation creates child binding too)
//   - 1 EdgeCalls (V1.22 modifier-in-Parent): Parent.check → SafeMath.add

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract Parent {
    using SafeMath for uint256;

    modifier check(uint256 amount) {
        amount.add(0); // V1.22 modifier param + V1.0 state-var-style dispatch
        _;
    }
}

contract Child is Parent {
    function run() external check(5) returns (uint256) {
        return 1;
    }
}
