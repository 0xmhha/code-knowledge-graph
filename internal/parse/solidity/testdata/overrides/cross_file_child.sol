// Sol W2 fixture (cross-file, child side) — overrides BaseVault.deposit
// declared in cross_file_parent.sol. Expected: EdgeOverrides emitted at
// ConfInferred because the resolution crosses file boundaries.
pragma solidity ^0.8.20;

contract ChildVault is BaseVault {
    function deposit(uint amount) public override returns (uint) {
        return amount * 2;
    }
}
