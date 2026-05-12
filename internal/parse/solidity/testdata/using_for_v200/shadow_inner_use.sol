// W-C W6 V2.0 fixture — line-range scope-aware lookup. inner-block
// shadow with use site INSIDE the inner block must resolve via the
// inner type. Pre-V2.0 (V1.30 V0 first-decl-wins) resolved all use
// sites to the outer type — outer-correctness only.
//
// Layout (with line numbers):
//   line 1-N: helpers
//   line M:   library Other { add(bytes32, bytes32) returns (bytes32); }
//   line M+: contract C using SafeMath for uint256, using Other for bytes32
//   line M+: function f(bool cond)
//   line M+:   uint256 x = 1;                       (outer x : uint256)
//   line M+:   if (cond) {
//   line M+:     bytes32 x = bytes32(0);            (inner x : bytes32)
//   line M+:     x.tag(bytes32(0));                 (USE: inner scope)
//   line M+:   }
//   line M+:   return x.add(1);                     (USE: outer scope)
//
// Expectations (V2.0 fix):
//   - 2 EdgeUsesFor: C → SafeMath, C → Other
//   - 2 EdgeCalls:
//     - C.f → SafeMath.add (outer scope use, line of return)
//     - C.f → Other.tag (inner scope use, line inside if-block)
//
// V1.30 V0 gets only 1 EdgeCalls (C.f → SafeMath.add); the inner use
// `x.tag(...)` drops because lookupReceiverType returns uint256
// (outer wins everywhere) and uint256 → Other binding doesn't exist.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

library Other {
    function tag(bytes32 a, bytes32 b) internal pure returns (bytes32) {
        return a ^ b;
    }
}

contract C {
    using SafeMath for uint256;
    using Other for bytes32;

    function f(bool cond) external pure returns (uint256) {
        uint256 x = 1;
        if (cond) {
            bytes32 x = bytes32(0);
            x.tag(bytes32(0));
        }
        return x.add(1);
    }
}
