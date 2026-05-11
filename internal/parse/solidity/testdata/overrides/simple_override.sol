// Sol W2 fixture — single virtual/override pair in one file.
// Expected:
//   Parent.foo SubKind="virtual"
//   Child.foo  SubKind="override"
//   EdgeOverrides: Child.foo → Parent.foo  (ConfExtracted, same file)
//   EdgeExtends:   Child     → Parent       (W1 regression — must still emit)
pragma solidity ^0.8.20;

contract Parent {
    function foo() public virtual returns (uint) {
        return 1;
    }
}

contract Child is Parent {
    function foo() public override returns (uint) {
        return 2;
    }
}
