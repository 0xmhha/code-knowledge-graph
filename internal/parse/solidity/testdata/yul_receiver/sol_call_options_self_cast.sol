// W-C W10 V18 fixture probe — call-options self-cast.
//
// Modern Solidity (0.7+) replaced the deprecated `.call.value(x)()`
// chain with `.call{value: x, gas: y}(...)`. The grammar node for
// `.call{...}` is call_options_expression, which sits between
// member_expression and call_expression — so the V8 walker's
// "parent must be call_expression" check at line 94 may or may not
// reach through. This fixture exercises the path so we can confirm.
//
//   - OptCall.fire    : payable(this).call{value: 0}("") — explicit
//                       value-and-gas options on a self-target.
//   - OptCall.fireGas : payable(this).call{gas: 50000}("") — gas-only
//                       options, mirror of the value-only form.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract OptCall {
    function fire() external payable {
        (bool ok, ) = payable(this).call{value: 0}("");
        require(ok, "self-call failed");
    }

    function fireGas() external payable {
        (bool ok, ) = payable(this).call{gas: 50000}("");
        require(ok, "self-call gas failed");
    }
}
