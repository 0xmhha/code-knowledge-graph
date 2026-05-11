// Sol W4 fixture — `library` declaration.
// Expected: NodeContract SubKind="library" for SafeMath, and its `add`
// function is qualified as "SafeMath.add" via nearestContractName.
pragma solidity ^0.8.20;

library SafeMath {
    function add(uint a, uint b) internal pure returns (uint) {
        return a + b;
    }

    function sub(uint a, uint b) internal pure returns (uint) {
        require(b <= a, "underflow");
        return a - b;
    }
}
