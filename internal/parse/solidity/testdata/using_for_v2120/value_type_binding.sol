// W-C W6 V2.12 fixture — Solidity 0.8.8+ user-defined value type (UDVT)
// as the receiver type for `using ... for ...`.
//
// UDVT semantics (0.8.8+ spec):
//   `type Amount is uint256;` declares Amount as a distinct type
//   wrapping uint256. The compiler enforces a strict barrier — no
//   implicit conversion to/from uint256, only explicit wrap/unwrap
//   via Amount.wrap(x) / Amount.unwrap(amt). UDVTs can serve as the
//   receiver type for `using Lib for Amount;`, and library functions
//   taking `Amount` parameters become method-style callable on
//   Amount values.
//
// V0 query's `source: (_) @type` captures whatever node the
// using_directive's `source` field points at. Tree-sitter-solidity
// v1.2.13 wraps UDVT references in `type_name` containing a
// `user_defined_type` child (same shape as cross-contract or
// library type references). `normaliseUsingForType` calls
// `extractTypeNameText` which should yield the bare identifier
// "Amount" — matching the NodeField.Signature output of a state
// variable declared `Amount x;`.
//
// Expectations:
//   - 1 EdgeUsesFor: Vault → Math (V0 type_alias capture of `Math`).
//   - 1 EdgeCalls: Vault.tick → Math.double via V1.0 state-var
//     dispatch on `balance` (typed `Amount`).
//   - Surround-safety: UDVT declaration shouldn't break sibling
//     declarations (Math library + functions, Vault + tick).

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.8;

type Amount is uint256;

library Math {
    function double(Amount a) internal pure returns (Amount) {
        return Amount.wrap(Amount.unwrap(a) * 2);
    }
}

contract Vault {
    using Math for Amount;

    Amount public balance;

    function tick() external {
        balance = balance.double();
    }
}
