// W-C W8 V13 fixture — mapping value as function pointer.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract MappingHolder {
    mapping(address => function(uint256) external returns (uint256)) handlers;

    function noop() external pure {}
}
