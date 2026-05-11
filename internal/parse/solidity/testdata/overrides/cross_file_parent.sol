// Sol W2 fixture (cross-file, parent side) — declares the virtual function
// that the child file (cross_file_child.sol) overrides. Resolution must
// produce ConfInferred edges because src/dst live in different files.
pragma solidity ^0.8.20;

contract BaseVault {
    function deposit(uint amount) public virtual returns (uint) {
        return amount;
    }
}
