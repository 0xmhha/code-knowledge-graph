// W-C W6 V1.17 baseline (regression guard) — no shadowing at all.
// Confirms that with no local/param of the same name, state-var
// resolution still works after the V1.17 precedence flip. Same
// behavior as V1.0 canonical test, included here so the V1.17 fix
// can't accidentally regress the state-var path.
//
// Expectations:
//   - 1 EdgeUsesFor: C → SafeMath
//   - 1 EdgeCalls (V1.0): C.f → SafeMath.add

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract C {
    using SafeMath for uint256;

    uint256 public balance;

    function f() external view returns (uint256) {
        // No local or param shadowing — must resolve via state-var.
        return balance.add(1);
    }
}
