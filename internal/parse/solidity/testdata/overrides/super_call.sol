// Sol W2 fixture — super.foo() in an override chain.
// Verifies (a) virtual_override SubKind on the middle of a 3-level chain,
// (b) EdgeOverrides emission walks one inheritance hop at a time (Mid → Base,
// Top → Mid). super.foo() body call resolution is W3 scope; W2 only checks
// the *declaration-time* overrides relationship.
//
// Expected (W2 declaration-time):
//   Base.greet      SubKind="virtual"
//   Mid.greet       SubKind="virtual_override"
//   Top.greet       SubKind="override"
//   EdgeOverrides:  Mid.greet → Base.greet  (Extracted)
//                   Top.greet → Mid.greet   (Extracted)
pragma solidity ^0.8.20;

contract Base {
    function greet() public virtual returns (uint) {
        return 1;
    }
}

contract Mid is Base {
    function greet() public virtual override returns (uint) {
        return super.greet() + 10;
    }
}

contract Top is Mid {
    function greet() public override returns (uint) {
        return super.greet() + 100;
    }
}
