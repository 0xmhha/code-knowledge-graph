// W-C W6 V1.25 fixture — Sol 0.7.4+ file-level free function plus a
// contract using-for binding in the same file. Three assertions:
//
//  1. Free function parses successfully (no panic / no error edges).
//  2. Free function does NOT produce phantom EdgeCalls — Sol's `using
//     for` directive is contract-scope only, so there is no binding
//     map for a free-function caller to consult.
//  3. The contract's using-for dispatch still resolves normally
//     alongside the free function — i.e. presence of a free function
//     doesn't disturb runFunctionDecl / Pass 1.5 / lookupReceiverType
//     for contract methods.
//
// Expectations:
//   - 1 EdgeUsesFor: C → SafeMath
//   - 1 EdgeCalls (contract path): C.useIt → SafeMath.add
//   - 0 EdgeCalls from `freeAdd` body (no binding scope for free
//     functions in current Sol grammar).

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

// File-level free function (Sol 0.7.4+). No contract scope → no `using`
// binding available. Even if the body wrote `x.add(1)`, dispatch
// resolution would drop (no contract-scope binding map for the caller).
function freeAdd(uint256 a, uint256 b) pure returns (uint256) {
    return a + b;
}

contract C {
    using SafeMath for uint256;

    function useIt(uint256 seed) external pure returns (uint256) {
        return seed.add(freeAdd(1, 2));
    }
}
