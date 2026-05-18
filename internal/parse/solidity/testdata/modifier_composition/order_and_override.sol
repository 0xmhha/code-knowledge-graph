// W-C W7.3 V0 fixture — modifier composition order + override.
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

abstract contract Base {
    modifier onlyOwner() virtual { _; }
    modifier nonReentrant() { _; }
    modifier checkAmount(uint256 v) virtual { _; }
}

contract Child is Base {
    // Modifier override: Child.onlyOwner overrides Base.onlyOwner.
    modifier onlyOwner() override { _; }
    modifier checkAmount(uint256 v) override { _; }

    // Multi-modifier function: 3 modifiers in source order.
    //   Order 0: nonReentrant
    //   Order 1: onlyOwner
    //   Order 2: checkAmount
    function withdraw(uint256 amount) external nonReentrant onlyOwner checkAmount(amount) {
    }
}
