pragma solidity ^0.8.20;

contract Vault {
    mapping(address => uint256) public balances;
    event Deposited(address indexed who, uint256 amount);

    modifier nonZero(uint256 v) { require(v > 0, "zero"); _; }

    function deposit(uint256 amount) external nonZero(amount) {
        balances[msg.sender] += amount;
        emit Deposited(msg.sender, amount);
    }
}
