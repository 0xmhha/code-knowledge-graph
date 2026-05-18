// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Wallet {
    address public owner;
    uint256 public balance;

    constructor() { owner = msg.sender; }

    function deposit(uint256 amount) external {
        balance += amount;
    }

    function withdraw(uint256 amount) external {
        require(msg.sender == owner, "not owner");
        balance -= amount;
    }
}
