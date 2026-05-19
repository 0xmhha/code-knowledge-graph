// W-C W6 V11 fixture — using directive whose target is an
// interface. Sol's reference compiler rejects this hierarchy
// (interfaces have no function bodies so binding is
// meaningless), but tree-sitter-solidity v1.2.11 accepts the
// syntactic shape. V11 audits that our resolver gracefully
// drops the binding rather than emitting a false-positive
// EdgeUsesFor to the interface.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

interface IFoo {
    function ping() external returns (bool);
}

contract Caller {
    using IFoo for address;

    function probe(address t) external returns (bool) {
        return t == address(0);
    }
}
