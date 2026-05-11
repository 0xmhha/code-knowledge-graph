// Sol W2 fixture — virtual function declared but never overridden.
// Verifies SubKind="virtual" is stamped even when no child overrides it,
// and that no spurious EdgeOverrides is emitted from a bare virtual decl.
//
// Expected:
//   Base.compute SubKind="virtual"
//   EdgeOverrides total = 0
pragma solidity ^0.8.20;

contract Base {
    function compute() public virtual returns (uint) {
        return 42;
    }

    function plain() public pure returns (uint) {
        return 0;
    }
}
