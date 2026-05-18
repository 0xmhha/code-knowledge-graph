// W8 V0 fixture — contract-type cast dispatch.
//
// W3 covers `IFoo(addr).bar()` when IFoo is an interface. W8 covers
// the parallel pattern when the cast target is a concrete contract:
// `MyContract(addr).method()` is a runtime address re-typed to call
// a method declared on MyContract.
//
// V0 emit: 1 EdgeInvokes per matched call_expression,
// DispatchKind = "contract_cast", ConfAmbiguous (runtime address
// determines the actual receiver).

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Vault {
    uint256 public balance;

    function deposit(uint256 v) external {
        balance += v;
    }

    function withdraw(uint256 v) external {
        require(balance >= v, "insufficient");
        balance -= v;
    }
}

contract Caller {
    function forward(address target, uint256 v) external {
        Vault(target).deposit(v);
        Vault(target).withdraw(v);
    }
}
