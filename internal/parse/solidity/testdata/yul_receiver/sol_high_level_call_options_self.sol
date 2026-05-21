// W-C W10 V22 fixture probe — high-level call-options self-call.
//
// V18 added a struct_expression hop on the low-level walker so
// `.call{value: x, gas: y}(...)` self-casts kept their marker.
// V19 introduced the high-level walker without that hop — so the
// equivalent typed syntax (`this.foo{value: x}()`) may silently
// fall through. This probe forces the question.
//
//   - OptHigh.fire     : this.target{value: 0}() — bare-this with
//                        value option.
//   - OptHigh.fireGas  : this.target{gas: 50000}() — gas-only option.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract OptHigh {
    function fire() external payable {
        this.target{value: 0}();
    }

    function fireGas() external payable {
        this.target{gas: 50000}();
    }

    function target() external payable {}
}
