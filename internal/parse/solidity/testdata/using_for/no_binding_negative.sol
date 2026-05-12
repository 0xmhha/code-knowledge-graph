// W-C W6 fixture — negative case. Contract has method calls on identifiers
// but no `using` directive. V0 must emit ZERO EdgeUsesFor edges.
//
// Defensive purpose: guards against a future regression where the detector
// false-positives on plain method-call syntax (e.g. classifying `obj.foo()`
// as evidence of a library binding without a using_directive present).

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Plain {
    uint256 public value;

    function update(uint256 v) external {
        value = v;
    }

    function readBack() external view returns (uint256) {
        return value;
    }
}
