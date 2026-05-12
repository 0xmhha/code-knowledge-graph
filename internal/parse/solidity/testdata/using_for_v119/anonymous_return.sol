// W-C W6 V1.19 baseline guard — anonymous return slot. No name on the
// return parameter; V1.19 paramType emit skips silently. V1.3
// funcReturnTypes still fires for the type (chain-call dispatch),
// but receiver-by-identifier is not addressable.
// Expectations:
//   - 1 EdgeUsesFor: C → SafeMath
//   - 1 EdgeCalls (V1.0 state-var path, not V1.19): C.f → SafeMath.add
//     (`x.add(1)` resolves via state-var x, not via return slot.)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract C {
    using SafeMath for uint256;

    uint256 public x;

    // Anonymous return slot — no name field. V1.19 emits nothing for
    // this slot. The receiver `x.add(1)` below resolves via state-var x.
    function f() external view returns (uint256) {
        return x.add(1);
    }
}
