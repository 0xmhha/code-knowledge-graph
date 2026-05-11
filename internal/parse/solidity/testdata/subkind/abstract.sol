// Sol W4 fixture — `abstract contract` declaration.
// Expected: NodeContract SubKind="abstract" for Base.
pragma solidity ^0.8.20;

abstract contract Base {
    function foo() public virtual returns (uint);

    function bar() public pure returns (uint) {
        return 42;
    }
}
