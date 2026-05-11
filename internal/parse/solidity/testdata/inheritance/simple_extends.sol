// Sol W1 fixture — simple single-parent inheritance (contract → contract).
// Expected edges (same file, ConfExtracted):
//   Child  extends Parent
pragma solidity ^0.8.20;

contract Parent {
    function ping() public pure returns (uint) { return 1; }
}

contract Child is Parent {
    function pong() public pure returns (uint) { return 2; }
}
