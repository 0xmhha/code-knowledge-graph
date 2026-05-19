// W-C W10 V13 fixture — selfdestruct(this) audit.
//
// `selfdestruct(payable(this))` destroys the contract and forwards
// the remaining balance to itself — semantically pointless since
// the contract is gone before the balance can land. V13 audits
// whether the existing walkers surface this as a low-level call
// or self-reentrant marker (the answer is no — selfdestruct is
// a separate Sol builtin, not a member-expression call).

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract SelfDestruct {
    function destroyToSelf() external {
        selfdestruct(payable(this));
    }

    function destroyToAddress(address payable target) external {
        selfdestruct(target);
    }
}
