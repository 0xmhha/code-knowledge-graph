// W-C W6 V1.23 fixture — constructor parameter as using-for receiver.
// `constructor(uint256 seed) { stored = seed.add(1); }` —
// constructor_definition is a distinct AST node (no name field, no
// queryConstructor pre-V1.23). V1.23 adds a graph node for the
// constructor (NodeFunction with synthetic name "constructor", qname
// "Container.constructor") plus the V1.22 meta-walker pattern so its
// parameter / local-var receivers dispatch through using-for.
//
// Expectations:
//   - 1 EdgeUsesFor: C → SafeMath
//   - 1 EdgeCalls (V1.23 constructor param): C.constructor → SafeMath.add

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract C {
    using SafeMath for uint256;

    uint256 public stored;

    constructor(uint256 seed) {
        stored = seed.add(1);
    }
}
