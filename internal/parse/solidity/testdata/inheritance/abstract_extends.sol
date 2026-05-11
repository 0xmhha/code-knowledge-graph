// Sol W1 fixture — abstract contract inheriting an interface (regression
// guard: the abstract keyword detector from W4 must not interfere with the
// W1 inheritance specifier walk). Expected edges (same file, ConfExtracted):
//   AbstractBase implements IThing
//   Concrete     extends    AbstractBase
pragma solidity ^0.8.20;

interface IThing {
    function thing() external returns (uint);
}

abstract contract AbstractBase is IThing {
    // declared but not implemented — concrete subclass fills in.
    function thing() external virtual returns (uint);
}

contract Concrete is AbstractBase {
    function thing() external override returns (uint) { return 7; }
}
