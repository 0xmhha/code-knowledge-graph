// Sol W1 fixture — multiple inheritance (Solidity supports diamond-style
// MRO via C3 linearisation; the parser captures the *direct* parents only,
// order-preserving). Expected edges (same file, ConfExtracted):
//   Child extends A
//   Child extends B
//   Child extends C
pragma solidity ^0.8.20;

contract A {
    function a() public pure returns (uint) { return 1; }
}

contract B {
    function b() public pure returns (uint) { return 2; }
}

contract C {
    function c() public pure returns (uint) { return 3; }
}

contract Child is A, B, C {
    function all() public pure returns (uint) { return 6; }
}
