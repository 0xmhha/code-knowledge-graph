// W-C W8 V26 fixture — cross-contract function pointer chain (negative lock).
//
// V6 (cross_contract_fn_pointer.go) explicitly documents this case
// as a deferred limitation:
//
//	V6 limitations:
//	  - Chained / cast receivers (`getHub().onAction(x)`,
//	    `Hub(addr).onAction(x)`) drop — matchStateVarMethodCall
//	    requires a bare-identifier receiver.
//
// `s.getCb()(x)` is a sibling of the documented chained-member case:
// the AST shape is `call_expression(call_expression(member s.getCb), x)`
// rather than `call_expression(member <expr>.method, args)`, so it
// falls through V6 entirely. V9 (return propagation) similarly only
// classifies a return value as fn-typed when the expression is a
// bare identifier resolvable to a fn-typed param or state-var; the
// return value of a cross-contract call (`s.getCb()`) does not
// participate.
//
// V26 *locks the current behaviour*: both false-negative cells are
// asserted explicitly so a future walker fix that introduces
// cross-contract fn-pointer return-type tracking flips these
// assertions and forces the author to flip this fixture to a
// positive lock. This is the same pattern as V21
// (chained_receiver_self_call.sol) — known limitations recorded as
// negative tests, not silently passing.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Source {
    function(uint256) external returns (uint256) stored;

    function getCb()
        external
        view
        returns (function(uint256) external returns (uint256))
    {
        return stored;
    }
}

contract Sink {
    Source s;

    // Cell 1: chained cross-contract invoke.
    //   `s.getCb()(x)` — V6 misses (chained receiver).
    //   HasFunctionPointerCall stays false.
    function chainInvoke(uint256 x) external returns (uint256) {
        return s.getCb()(x);
    }

    // Cell 2: chained cross-contract return propagation.
    //   `return s.getCb();` — V9 misses (return value of cross-
    //   contract call is not classified as fn-typed by the param /
    //   state-var lookup).
    //   HasFunctionPointerPropagation stays false.
    function chainFetchOnly()
        external
        view
        returns (function(uint256) external returns (uint256))
    {
        return s.getCb();
    }
}
