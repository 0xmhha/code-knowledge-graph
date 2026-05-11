// Sol W2 fixture — multiple inheritance with explicit override(A, B).
// Solidity requires the override list when a function inherits from more
// than one base. The detector must emit one EdgeOverrides per listed
// parent (one ref per parent in runFunctionDecl; one edge per ref).
//
// Expected:
//   A.foo SubKind="virtual"
//   B.foo SubKind="virtual"
//   C.foo SubKind="override"
//   EdgeOverrides: C.foo → A.foo  (Extracted)
//                  C.foo → B.foo  (Extracted)
//   EdgeExtends:   C    → A       (W1 regression)
//                  C    → B       (W1 regression)
pragma solidity ^0.8.20;

contract A {
    function foo() public virtual returns (uint) {
        return 1;
    }
}

contract B {
    function foo() public virtual returns (uint) {
        return 2;
    }
}

contract C is A, B {
    function foo() public override(A, B) returns (uint) {
        return 3;
    }
}
